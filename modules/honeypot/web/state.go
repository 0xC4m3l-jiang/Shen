package web

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// 本文件是**受限写路径与写后读**（方案 `C06`）的状态机：
//
//	① 状态**按合成会话隔离**（登录时从场景基础数据拷一份）⇒ 同会话写后读可见、跨会话不串；
//	② 动作集**受限**（只改场景里的合成对象：账号启用状态 / 配置项取值），**永不执行真实命令/SQL**；
//	③ 提交是**幂等**的（值没变就不提交、不追加审计、不推进版本）并支持 **CAS**（版本不符 ⇒ 409，可重试不双写）；
//	④ 资源**有配额**（每会话写次数上限 + 审计条目上限），越限受控拒绝（`429`），不做无界增长；
//	⑤ 状态**进程内、可丢失**（随会话 TTL / 容量淘汰一起消失），不落盘、不外发（与模块的"无出站面"一致）。

// 三类的**受控错误**：调用方据此映射状态码（不靠字符串匹配）。
var (
	// errNotConfigured：动作指向不存在的合成对象 ⇒ 404。
	errNotConfigured = errors.New("场景里没有这个对象")
	// errInvalidValue：取值非法（例如 `enabled` 不是布尔、配置值为空）⇒ 400。
	errInvalidValue = errors.New("取值非法")
	// errWriteQuota：本会话写次数或审计条目已到上限 ⇒ 429（受控拒绝，不是 500）。
	errWriteQuota = errors.New("本会话的写入配额已用尽")
	// errVersionConflict：CAS 版本不符 ⇒ 409（可重试，重试不会双写）。
	errVersionConflict = errors.New("版本不符")
)

// defaultMaxWritesPerSession 是单会话允许提交的写操作数上限（资源配额）。
const defaultMaxWritesPerSession = 200

// defaultMaxAuditEntries 是单会话审计条目的上限：写操作会追加审计，追加到上限即拒绝后续写入。
const defaultMaxAuditEntries = 200

// sessionState 是一个合成会话的**可变场景状态**（写后读的作用域就是它）。
//
// 为什么按会话而不是全局：诱饵的意义在于"每个来访者看到自己的那一份"，把 A 的改动暴露给 B 既不像真实
// 管理台里的"共享后台"，也会让**跨会话串数据**成为一个可被对手利用的观测面（方案 `C05` 的"租户不串数据"）。
type sessionState struct {
	expires time.Time
	users   []User
	config  []ConfigItem
	audit   []AuditEvent

	// version 每次**成功提交**递增（CAS 的版本号；读取时随 JSON 返回）。
	version uint64
	// writes 已提交的写操作数（配额）。
	writes int
	// actor 是这次合成会话对外的"操作者"（登录时提交的用户名；用于审计条目的 actor）。
	actor string
}

// newSessionState 由场景基础数据**深拷贝**出一份会话状态。
//
// 深拷贝是必需的：`Scenario` 的切片若只拷头部，两个会话会共享同一个底层数组 ——
// 那正是"跨会话串数据"。元素都是值类型（无指针字段），逐个复制即安全。
func newSessionState(sc Scenario, actor string, expires time.Time) *sessionState {
	return &sessionState{
		expires: expires,
		users:   append([]User(nil), sc.Users...),
		config:  append([]ConfigItem(nil), sc.Config...),
		audit:   append([]AuditEvent(nil), sc.Audit...),
		actor:   actor,
	}
}

// latestFirst 返回按时间**倒序**的副本（审计日志的读视图）。
//
// 为什么审计要倒序（`C06`/`T19`）：真实审计日志都是"最新在前"，而且**刚发生的变更必须在第一页可见** ——
// 否则"写后读"虽然在数据上成立，运维/对手都看不到自己的那一条（等于验证不了）。
// 只反转**读视图**，存储依然是追加序（追加是 O(1)，且顺序本身就是"发生过什么"的记录）。
func latestFirst(events []AuditEvent) []AuditEvent {
	out := make([]AuditEvent, len(events))
	for i := range events {
		out[i] = events[len(events)-1-i]
	}
	return out
}

// findUser / findConfig 按标识查下标（未找到 ⇒ errNotConfigured ⇒ 404）。
func (s *sessionState) findUser(id string) (int, error) {
	for i := range s.users {
		if s.users[i].ID == id {
			return i, nil
		}
	}
	return -1, fmt.Errorf("%w：账号 %q", errNotConfigured, id)
}

func (s *sessionState) findConfig(key string) (int, error) {
	for i := range s.config {
		if s.config[i].Key == key {
			return i, nil
		}
	}
	return -1, fmt.Errorf("%w：配置项 %q", errNotConfigured, key)
}

// guard 做提交前的统一检查：配额 + CAS。
//
// 顺序刻意的：先配额（资源护栏，与内容无关）再 CAS（并发语义）—— 配额用尽时不该因为版本恰好相符就放行。
func (s *sessionState) guard(expected *uint64) error {
	if s.writes >= defaultMaxWritesPerSession || len(s.audit) >= defaultMaxAuditEntries {
		return fmt.Errorf("%w（已写 %d 次，审计 %d 条）", errWriteQuota, s.writes, len(s.audit))
	}
	if expected != nil && *expected != s.version {
		return fmt.Errorf("%w：期望 v%d，当前 v%d", errVersionConflict, *expected, s.version)
	}
	return nil
}

// commit 提交一次写操作：追加审计 + 推进版本 + 计数。
//
// **只有值真的变了**才调用它（幂等：同值重复提交不产生审计、不推进版本、不消耗配额）。
func (s *sessionState) commit(at time.Time, action, target string) {
	s.audit = append(s.audit, AuditEvent{
		At: at.UTC().Format(time.RFC3339), Actor: s.actor, Action: action, Target: target,
	})
	s.version++
	s.writes++
}

// setUserEnabled 是受限动作之一：启用/禁用一个合成账号。
//
// 返回 (是否发生变化, 错误)。值未变 ⇒ `false, nil`（幂等成功，不消耗配额）。
func (s *sessionState) setUserEnabled(id string, enabled bool, at time.Time, expected *uint64) (bool, error) {
	i, err := s.findUser(id)
	if err != nil {
		return false, err
	}
	if s.users[i].Enabled == enabled {
		return false, nil
	}
	if err := s.guard(expected); err != nil {
		return false, err
	}
	s.users[i].Enabled = enabled
	if enabled {
		s.commit(at, "user.enable", id)
	} else {
		s.commit(at, "user.disable", id)
	}
	return true, nil
}

// setConfigValue 是受限动作之二：改一个合成配置项的取值。
//
// 取值约束（最小但必要）：非空、长度上限 512 —— 防止把会话状态当成任意大小的存储（资源配额的一部分）。
func (s *sessionState) setConfigValue(key, value string, at time.Time, expected *uint64) (bool, error) {
	if strings.TrimSpace(value) == "" || len(value) > maxConfigValueLen {
		return false, fmt.Errorf("%w：配置值必须非空且不超过 %d 字节", errInvalidValue, maxConfigValueLen)
	}
	i, err := s.findConfig(key)
	if err != nil {
		return false, err
	}
	if s.config[i].Value == value {
		return false, nil
	}
	if err := s.guard(expected); err != nil {
		return false, err
	}
	s.config[i].Value = value
	s.commit(at, "config.update", key)
	return true, nil
}

// maxConfigValueLen 是合成配置值的长度上限（配额：会话状态不能被写成任意大小的存储）。
const maxConfigValueLen = 512

// parseVersion 解析 CAS 版本参数（缺省 = 不做 CAS）。
//
// 严格解析（整串数字）：`12abc` 这类输入按非法处理，不"尽量解析"—— 那种宽容正是边界用例测不出来的原因。
func parseVersion(raw string) (*uint64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	n, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%w：版本号 %q 不是无符号整数", errInvalidValue, raw)
	}
	return &n, nil
}

// parseEnabled 解析 `enabled` 的取值：接受 true/false/1/0/on/off/enabled/disabled（**其余一律非法**）。
//
// 为什么不用宽松解析：`enabled=yes-please` 被当成 false 会让"写成功了但值不对"变成静默行为。
func parseEnabled(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "1", "on", "enabled", "enable":
		return true, nil
	case "false", "0", "off", "disabled", "disable":
		return false, nil
	default:
		return false, fmt.Errorf("%w：enabled=%q（用 true/false）", errInvalidValue, raw)
	}
}
