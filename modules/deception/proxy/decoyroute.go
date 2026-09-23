// 本文件是**专属诱饵路由**：把「已登记的诱饵路径」映射到幻境后端（方案 §9.2 / C01）。
//
// 它与判定完全无关，是**路由归属**（谁拥有这个路径），不是评分：
//
//	判定（judge/director）        → 按观测算分，产出三值
//	诱饵路由（本文件）            → 静态归属：这条路径属于某个诱饵 ⇒ 直接投递，不问核心
//
// 三条硬约束（都来自方案 §9.2，逐条都有测试）：
//
//	① **归一化 + 路径段边界**：`/admin` 不命中 `/administrator`；`/static/../.git/config` 命中 `/.git`；
//	② **最长匹配优先**：`/.git` 与 `/.git/config` 同时登记时后者胜出；
//	③ **故障绝不回生产**：诱饵后端不可用时返回固定错误，**禁止**回落业务 ——
//	   已经进了诱饵的请求再回源，会让对手看见「幻境崩了、真站在后面」，而且可能重复副作用。
//
// ⚠️ 归一化与段边界的实现在这里**各自一份**（适配器不能 import `common/core/internal/`，`ST-3`）：
// 核心侧对应的是 `contract.NormalizePath` / `contract.PathSegmentPrefix`，
// 语义必须一致 —— 改其一时**必须**同时改另一处与 `docs/spec/config.md` §2.4。
package proxy

import (
	"path"
	"strings"
)

// normalizeRoutePath 把路径归一化成**匹配视图**（与核心 `contract.NormalizePath` 同语义）。
func normalizeRoutePath(p string) string {
	if p == "" {
		return ""
	}
	return path.Clean(p)
}

// routeSegmentPrefix 判断 got 是否位于路径段 want 之下（与核心 `contract.PathSegmentPrefix` 同语义）。
func routeSegmentPrefix(got, want string) bool {
	if want == "" {
		return false
	}
	seg := strings.TrimSuffix(want, "/")
	if seg == "" {
		return got == "/" || got == ""
	}
	return got == seg || strings.HasPrefix(got, seg+"/")
}

// matchDecoyRoute 取**最长**的那条命中路由。
//
// 为什么自己比长短、而不是「按表序取第一条」：依赖调用方先排好序是**隐式前提** ——
// 谁哪天把表换成一版没排序的（或在别处提前返回），会静默投递到**较短**的诱饵上，
// 而那种错在运行期几乎看不出来（两条诱饵都"看起来对"）。这里多比一次长度，前提就消失了。
// 表仍按长度降序（`applyEdgePolicy` 排的）：同长度时取表序靠前者 ⇒ 结果确定（`AR-30`）。
//
// routes 为空、路径为空、或没有命中 ⇒ ok=false（调用方照常走判定/白名单/兜底）。
func matchDecoyRoute(routes []policyDecoyRoute, reqPath string) (policyDecoyRoute, bool) {
	if len(routes) == 0 {
		return policyDecoyRoute{}, false
	}
	req := normalizeRoutePath(reqPath)
	if req == "" {
		return policyDecoyRoute{}, false
	}
	var best policyDecoyRoute
	found := false
	for _, r := range routes {
		if !routeSegmentPrefix(req, r.path) {
			continue
		}
		if !found || len(r.path) > len(best.path) {
			best, found = r, true
		}
	}
	return best, found
}
