package proxy

// 本文件是**升级 Caddy 时的安全网**（用户要求：升级要快、且不得静默坏）。
//
// 思路：把「我们**依赖**的 Caddy 行为」逐条写成断言。升级后 `make gate` 一旦红，
// 红的那一行就直接告诉你**哪一条假设变了**，不用去猜哪里坏了。
//
// 为什么必须这样：我们对 Caddy 的依赖里，有几条**不会造成编译错误**——
// 它们在升级后照样编译、照样启动，只是行为变了，然后在生产上悄悄失效。

import (
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

// ── 编译期断言：Handler 必须继续满足 Caddy 的四类契约 ────────────────────────
//
// 这几条若在升级后不成立，会**编译失败**（好事：立刻可见）。
var (
	_ caddy.Module                = (*Handler)(nil)
	_ caddy.Provisioner           = (*Handler)(nil)
	_ caddy.Validator             = (*Handler)(nil)
	_ caddy.CleanerUpper          = (*Handler)(nil)
	_ caddyhttp.MiddlewareHandler = (*Handler)(nil)
)

// TestCaddyAssumptionsStillHold 锁定四条**不会引发编译错误**的依赖。
func TestCaddyAssumptionsStillHold(t *testing.T) {
	// ① Caddy 给响应加的默认 `Server` 头值。
	//
	// 我们的 headerSanitizer 只在 `Server` **恰好等于这个值**时删它（上游给了就保留）。
	// 若 Caddy 改了默认值 → 我们的清洗会静默失效 → 攻击者又能看到代理栈指纹（OH-2）。
	if caddyhttp.ServerHeader != caddyDefaultServerHeader {
		t.Fatalf("Caddy 的默认 Server 头变了：%q（我们期待 %q）。"+
			"请同步 deception/proxy/handler.go 的 caddyDefaultServerHeader，否则 OH-2 清洗会静默失效",
			caddyhttp.ServerHeader, caddyDefaultServerHeader)
	}

	// ② `caddy.Duration` 的单位是纳秒。
	//
	// 我们的配置字段全用它（判定超时 / 缓存 TTL / 时间窗）。单位若变，阈值会整体错位，
	// 而且是那种「跑得通但数值全错」的错。
	if got := time.Duration(caddy.Duration(time.Second)); got != time.Second {
		t.Fatalf("caddy.Duration 的单位变了：time.Second 得到 %v", got)
	}

	// ③④ 我们在 BuildConfig 里用**原始 JSON** 引用了两个标准模块的 ID：
	//     · `headers`（错误路径删 Server / Via）
	//     · `reverse_proxy`（转发）
	// 名字若变，配置会**启动失败**（这个还算可见），但报错信息会指向 Caddy 内部，
	// 所以在这里先给出人话。
	for _, id := range []string{"http.handlers.headers", "http.handlers.reverse_proxy", "http.handlers.shen_proxy"} {
		if _, err := caddy.GetModule(id); err != nil {
			t.Fatalf("模块 %q 未注册：%v（BuildConfig 里的原始 JSON 引用了它，升级后需同步）", id, err)
		}
	}
}
