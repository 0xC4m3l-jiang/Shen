package deception

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// seedDoc 只取部署配置里归管控台所有的五个域（宽松解码：其余键一律忽略 ——
// 部署配置的完整校验是核心的职责，这里只是"把当前生效的登记数据读出来给人看"）。
type seedDoc struct {
	Honeypots []struct {
		Name    string `yaml:"name"`
		Type    string `yaml:"type"`
		Addr    string `yaml:"addr"`
		Enabled bool   `yaml:"enabled"`
	} `yaml:"honeypots"`
	Decoys *struct {
		Assets []struct {
			ID      string   `yaml:"id"`
			Kind    string   `yaml:"kind"`
			Path    string   `yaml:"path"`
			Hosts   []string `yaml:"hosts"`
			Content string   `yaml:"content"`
			Backend string   `yaml:"backend"`
			Enabled bool     `yaml:"enabled"`
		} `yaml:"assets"`
	} `yaml:"decoys"`
	Whitelist struct {
		SourceCIDRs  []string `yaml:"source_cidrs"`
		UserAgents   []string `yaml:"user_agents"`
		PathPrefixes []string `yaml:"path_prefixes"`
	} `yaml:"whitelist"`
	Blacklist []BlackRuleYAML `yaml:"blacklist"`
	Injects   *[]struct {
		Kind    string `yaml:"kind"`
		Snippet string `yaml:"snippet"`
		Marker  string `yaml:"marker"`
	} `yaml:"injects"`
}

// BlackRuleYAML 是 blacklist[] 的 yaml 形态。
type BlackRuleYAML struct {
	ID         string `yaml:"id"`
	PathPrefix string `yaml:"path_prefix"`
	Reason     string `yaml:"reason"`
}

// ParseSeed 从部署配置字节中取出五个域，返回初始数据集与其内容校验和（用于检测基线漂移）。
func ParseSeed(raw []byte) (Dataset, string, error) {
	var doc seedDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return Dataset{}, "", fmt.Errorf("deception: 解析部署配置失败：%w", err)
	}
	ds := Dataset{}
	for _, h := range doc.Honeypots {
		ds.Honeypots = append(ds.Honeypots, Honeypot{Name: h.Name, Type: h.Type, Addr: h.Addr, Enabled: h.Enabled})
	}
	if doc.Decoys != nil {
		for _, a := range doc.Decoys.Assets {
			ds.Decoys = append(ds.Decoys, DecoyAsset{ID: a.ID, Kind: a.Kind, Path: a.Path,
				Hosts: append([]string{}, a.Hosts...), Content: a.Content, Backend: a.Backend, Enabled: a.Enabled})
		}
	}
	ds.Whitelist = Whitelist{
		SourceCIDRs: doc.Whitelist.SourceCIDRs, UserAgents: doc.Whitelist.UserAgents, PathPrefixes: doc.Whitelist.PathPrefixes,
	}
	for _, b := range doc.Blacklist {
		ds.Blacklist = append(ds.Blacklist, BlackRule(b))
	}
	if doc.Injects != nil {
		ds.InjectsProvided = true
		for _, r := range *doc.Injects {
			ds.Injects = append(ds.Injects, Inject{Kind: r.Kind, Snippet: r.Snippet, Marker: r.Marker})
		}
	}
	ds = ds.normalizeNil()
	return ds, contentChecksum(ds), nil
}

// ReadSeedFile 读取并解析部署配置文件（不存在返回 os.ErrNotExist）。
func ReadSeedFile(path string) (Dataset, string, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- 路径来自运维配置的环境变量（只读挂载）
	if err != nil {
		return Dataset{}, "", err
	}
	return ParseSeed(raw)
}

// contentChecksum 是五个内容域的摘要（与版本号 / 元数据无关）。
func contentChecksum(ds Dataset) string {
	body := struct {
		H []Honeypot   `json:"h"`
		D []DecoyAsset `json:"d"`
		W Whitelist    `json:"w"`
		B []BlackRule  `json:"b"`
		I []Inject     `json:"i"`
		P bool         `json:"p"`
	}{ds.Honeypots, ds.Decoys, ds.Whitelist, ds.Blacklist, ds.Injects, ds.InjectsProvided}
	raw, _ := json.Marshal(body)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// SeedOutcome 是启动时自动初始化的结论（供启动日志与系统状态展示）。
type SeedOutcome struct {
	Source      string `json:"source"`
	Seeded      bool   `json:"seeded"`  // 本次启动完成了初始化
	Missing     bool   `json:"missing"` // 未配置或找不到部署配置
	Drifted     bool   `json:"drifted"` // 已接管后部署配置的这些域又被改过（不再生效）
	Error       string `json:"error,omitempty"`
	Initialized bool   `json:"initialized"`
}

// AutoSeed 在数据集尚未接管时用部署配置初始化；已接管时检测基线漂移。永不因部署配置问题阻断启动。
func AutoSeed(s *Store, path string) SeedOutcome {
	out := SeedOutcome{Source: path}
	cur := s.Get()
	if path == "" {
		out.Missing, out.Initialized = true, cur.Initialized
		return out
	}
	seed, sum, err := ReadSeedFile(path)
	if err != nil {
		out.Missing = os.IsNotExist(err)
		if !out.Missing {
			out.Error = err.Error()
		}
		out.Initialized = cur.Initialized
		return out
	}
	if cur.Initialized {
		out.Initialized = true
		out.Drifted = cur.SeedChecksum != "" && cur.SeedChecksum != sum
		return out
	}
	ok, err := s.Seed(seed, path, sum)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	out.Seeded, out.Initialized = ok, true
	return out
}
