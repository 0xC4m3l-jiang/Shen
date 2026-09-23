package policy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"shen/common/core/internal/contract"
	"shen/common/core/internal/store"
)

// AI 能力服务与欺骗内容：**配置校验** + **清单装载** + **内容库落地**。
//
// 三方角色的分工（[`../../docs/spec/ai-contract.md`](../../docs/spec/ai-contract.md) §0）：
//
//	生成期（Python）        本文件（核心装载期）              消费期（适配器）
//	过护栏才入库    →   验校验和 / 单条上限 / variants  →   命中 + 再验 + 注入改道侧
//
// 这里**不做判定、不做决策**：它只把「生成侧产出的内容」变成「核心可下发的策略投影」（`AR-13` / `ST-8`）。

const (
	// ContentManifestSchemaVersion 是本端**能读懂**的清单格式版本。
	// 读到别的值一律拒绝装载 —— 猜错的代价是「按看不懂的清单把内容注入到别人面前」（领域禁猜）。
	ContentManifestSchemaVersion = 1

	// ContentSelectorSession 是当前**唯一**合法的变体选择器。
	ContentSelectorSession = "session"

	// MaxContentBodyBytes 是单条内容体上限（字节）。超限**丢弃该条**并 warn（不整份作废）。
	MaxContentBodyBytes = 65536

	// maxManifestBytes 是清单文件的总字节上限。超限即**拒绝装载**（提示做分片 —— 阶段 B）。
	maxManifestBytes = 1 << 20

	// 默认值（与 docs/spec/config.md §2.13 一致）。
	defaultAIVariants       = 8
	defaultAIRotateCooldown = 30 * time.Minute
)

// aiDoc 是配置文档的 `ai:` 段。**整段可选**（nil = 未配置 = 全关）。
//
// 必填项在段内一律用指针：nil 表示「键不存在」，与「值为零值」区分开（同 configDoc 的规矩）。
type aiDoc struct {
	Enabled  *bool         `yaml:"enabled"`
	Kinds    *[]string     `yaml:"kinds"`
	Model    *string       `yaml:"model"`
	Manifest *string       `yaml:"manifest"`
	Content  *aiContentDoc `yaml:"content"`
}

type aiContentDoc struct {
	Variants       *int    `yaml:"variants"`
	RotateCooldown *string `yaml:"rotate_cooldown"`
}

// validateAI 校验 `ai:` 段；nil（未配置）是合法的，此时按默认值构造（全关）。
func (d *configDoc) validateAI() error {
	if d.AI == nil {
		return nil
	}
	// kinds 的形状**不看开关**：本项目的配置纪律是「非法值一律拒绝，不管它当下是否被消费」
	// （同 configDoc 的未知键拒绝）—— 写错了却不报、等打开开关时才爆，那才是坑。
	if d.AI.Kinds != nil {
		seen := map[string]bool{}
		for i, k := range *d.AI.Kinds {
			if strings.TrimSpace(k) == "" {
				return fmt.Errorf("policy: ai.kinds[%d] 不得为空", i)
			}
			if seen[k] {
				return fmt.Errorf("policy: ai.kinds[%d]=%q 重复", i, k)
			}
			seen[k] = true
		}
	}
	if d.AI.Content != nil && d.AI.Content.Variants != nil && *d.AI.Content.Variants < 1 {
		return fmt.Errorf("policy: ai.content.variants=%d 必须 ≥ 1（变体数不能为零）",
			*d.AI.Content.Variants)
	}
	if d.AI.Content != nil && d.AI.Content.RotateCooldown != nil {
		raw := strings.TrimSpace(*d.AI.Content.RotateCooldown)
		dur, err := time.ParseDuration(raw)
		if err != nil {
			return fmt.Errorf("policy: ai.content.rotate_cooldown=%q 不是合法时长：%w", raw, err)
		}
		if dur <= 0 {
			return fmt.Errorf("policy: ai.content.rotate_cooldown=%q 必须 > 0", raw)
		}
	}
	return nil
}

// buildAI 把 `ai:` 段构造成运行时配置（未配置即默认全关）。
func (d *configDoc) buildAI() contract.AIConfig {
	out := contract.AIConfig{
		Kinds: []string{"content"},
		Content: contract.AIContentConfig{
			Variants:       defaultAIVariants,
			RotateCooldown: defaultAIRotateCooldown,
		},
	}
	if d.AI == nil {
		return out
	}
	out.Enabled = d.AI.Enabled != nil && *d.AI.Enabled
	if d.AI.Kinds != nil && len(*d.AI.Kinds) > 0 {
		out.Kinds = slices.Clone(*d.AI.Kinds)
	}
	if d.AI.Model != nil {
		out.Model = *d.AI.Model
	}
	if d.AI.Manifest != nil {
		out.ManifestPath = *d.AI.Manifest
	}
	if d.AI.Content != nil {
		if d.AI.Content.Variants != nil {
			out.Content.Variants = *d.AI.Content.Variants
		}
		if d.AI.Content.RotateCooldown != nil {
			// validateAI 已保证可解析，故此处忽略错误是安全的。
			if dur, err := time.ParseDuration(strings.TrimSpace(*d.AI.Content.RotateCooldown)); err == nil {
				out.Content.RotateCooldown = dur
			}
		}
	}
	return out
}

// AI 返回 `ai:` 段的运行时配置（`MD-6`：纯计算，不读文件、不碰存储）。
func (l *Loader) AI(context.Context) (contract.AIConfig, error) {
	return l.ai, nil
}

// ── 清单装载 ────────────────────────────────────────────────────────────────

// manifestDoc 是清单文件的线格式（[`../../docs/spec/ai-contract.md`](../../docs/spec/ai-contract.md) §3）。
type manifestDoc struct {
	ManifestVersion int                     `json:"manifest_version"`
	Version         uint64                  `json:"version"`
	Selector        string                  `json:"selector"`
	Variants        int                     `json:"variants"`
	GeneratedAt     string                  `json:"generated_at"`
	Generator       string                  `json:"generator"`
	Entries         []contract.ContentEntry `json:"entries"`
	Skipped         []map[string]string     `json:"skipped"`
}

// LoadContentManifest 读 + 校验内容清单文件。
//
// 严格度与配置文件一致（`ST-21` 的精神）：**任一结构性问题即返回错误**，由调用方让进程启动失败。
// 只有两类问题是「丢该条 + warn」而不是整份作废：
//
//	① 单条内容体超过 `MaxContentBodyBytes`；
//	② 单条 `checksum` 与内容不符（宁可漏注入，不可注入错内容）。
//
// wantVariants 来自配置 `ai.content.variants`：**必须**与清单的 `variants` 一致，
// 否则「生成侧发的 N」与「核心以为的 N」不同，变体选择会在两边算出不同的槽位。
func LoadContentManifest(path string, wantVariants int) (contract.ContentManifest, []string, error) {
	var out contract.ContentManifest
	if strings.TrimSpace(path) == "" {
		return out, nil, errors.New("policy: 内容清单路径为空")
	}
	f, err := os.Open(path)
	if err != nil {
		return out, nil, fmt.Errorf("policy: 打开内容清单失败：%w", err)
	}
	defer func() { _ = f.Close() }()

	raw, err := io.ReadAll(io.LimitReader(f, maxManifestBytes+1))
	if err != nil {
		return out, nil, fmt.Errorf("policy: 读内容清单失败：%w", err)
	}
	if len(raw) > maxManifestBytes {
		return out, nil, fmt.Errorf(
			"policy: 内容清单超过 %d 字节上限 —— 需要分片拉取（阶段 B，见 ADR-0023 未解决 5）", maxManifestBytes)
	}

	var doc manifestDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return out, nil, fmt.Errorf("policy: 内容清单不是合法 JSON：%w", err)
	}
	if doc.ManifestVersion != ContentManifestSchemaVersion {
		return out, nil, fmt.Errorf("policy: 内容清单 manifest_version=%d，本端只支持 %d（拒绝装载，领域禁猜）",
			doc.ManifestVersion, ContentManifestSchemaVersion)
	}
	if doc.Selector != ContentSelectorSession {
		return out, nil, fmt.Errorf("policy: 内容清单 selector=%q，本端只支持 %q",
			doc.Selector, ContentSelectorSession)
	}
	if doc.Variants < 1 {
		return out, nil, fmt.Errorf("policy: 内容清单 variants=%d 必须 ≥ 1", doc.Variants)
	}
	if wantVariants > 0 && doc.Variants != wantVariants {
		return out, nil, fmt.Errorf("policy: 内容清单 variants=%d 与配置 ai.content.variants=%d 不一致",
			doc.Variants, wantVariants)
	}
	if len(doc.Entries) == 0 {
		// 空清单合法：投影时不带 content_manifest，适配器一律 no_content（不是错误）。
		return contract.ContentManifest{Version: doc.Version, Selector: doc.Selector, Variants: doc.Variants},
			nil, nil
	}

	seen := map[string]bool{}
	var dropped []string
	entries := make([]contract.ContentEntry, 0, len(doc.Entries))
	for i, entry := range doc.Entries {
		if strings.TrimSpace(entry.Resource) == "" {
			return out, nil, fmt.Errorf("policy: 内容清单 entries[%d].resource 为空", i)
		}
		if seen[entry.Resource] {
			return out, nil, fmt.Errorf("policy: 内容清单里资源 %q 重复出现", entry.Resource)
		}
		seen[entry.Resource] = true
		bodies := make([]contract.ContentBody, 0, len(entry.Bodies))
		seenVariant := map[int]bool{}
		for _, body := range entry.Bodies {
			where := fmt.Sprintf("%s#%d", entry.Resource, body.VariantID)
			if body.VariantID < 0 || body.VariantID >= doc.Variants {
				return out, nil, fmt.Errorf("policy: 内容清单 %s 的 variant_id=%d 落在 [0,%d) 之外",
					where, body.VariantID, doc.Variants)
			}
			if seenVariant[body.VariantID] {
				return out, nil, fmt.Errorf("policy: 内容清单 %s 的 variant_id 重复", where)
			}
			seenVariant[body.VariantID] = true
			if strings.TrimSpace(body.Body) == "" {
				return out, nil, fmt.Errorf("policy: 内容清单 %s 的内容体为空", where)
			}
			if len(body.Body) > MaxContentBodyBytes {
				dropped = append(dropped, fmt.Sprintf("%s（内容体 %d 字节 > 上限 %d）",
					where, len(body.Body), MaxContentBodyBytes))
				continue
			}
			if got := checksumOfContent(body.Body); !strings.EqualFold(got, body.Checksum) {
				dropped = append(dropped, fmt.Sprintf("%s（校验和不符：清单 %s，实算 %s）",
					where, body.Checksum, got))
				continue
			}
			// AR-22 的**读侧**防线：生成侧的护栏是第一道闸，这里不假设「来源一定经过护栏」——
			// 清单文件可能被手工改过、或被中间的构建步骤改过（校验和只能证明「与清单一致」，
			// 不能证明「内容合格」）。命中泄露/自曝类就丢这一条，并如实记原因。
			if err := ValidateContentBody([]byte(body.Body)); err != nil {
				dropped = append(dropped, fmt.Sprintf("%s（%v）", where, err))
				continue
			}
			bodies = append(bodies, body)
		}
		if len(bodies) == 0 {
			continue // 该资源一条可用内容都没有 → 不投影这个资源
		}
		entries = append(entries, contract.ContentEntry{
			Resource:  entry.Resource,
			ProfileID: entry.ProfileID,
			Bodies:    bodies,
		})
	}
	return contract.ContentManifest{
		Version:  doc.Version,
		Selector: doc.Selector,
		Variants: doc.Variants,
		Entries:  entries,
	}, dropped, nil
}

// SeedContentStore 把清单里的内容写进内容库（键 = 一致性键，见 `contract.ContentKey`）。
//
// 这是 `store.ContentStore` 的**第一个真实消费方**：装载期 `Put`，投影期 `Get`（热路径只读）。
// 返回写入条数。任何一条写失败即返回错误 —— 内容库不完整会让投影静默缺内容。
func SeedContentStore(ctx context.Context, cs store.ContentStore, m contract.ContentManifest) (int, error) {
	if cs == nil {
		return 0, errors.New("policy: 内容库不能为 nil")
	}
	n := 0
	for _, entry := range m.Entries {
		for _, body := range entry.Bodies {
			key := contract.ContentKey(entry.ProfileID, entry.Resource, body.VariantID, m.Version)
			// TTL 为 0 = 不过期：内容的有效期由策略版本决定（轮换即新版本），不由时间决定。
			if err := cs.Put(ctx, key, []byte(body.Body), 0); err != nil {
				return n, fmt.Errorf("policy: 写入内容库失败（%s）：%w", key, err)
			}
			n++
		}
	}
	return n, nil
}

// checksumOfContent 计算内容体的 SHA-256（小写十六进制）—— 与生成侧的 `checksum_of` 同义。
func checksumOfContent(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}
