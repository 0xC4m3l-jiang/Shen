package honeypot

import "fmt"

// 本文件集中放本模块的错误值：它们是对外可见的行为约定（调用方按类型/值判断），
// 因此单独成文件，避免散落在实现里。

// errNilProtocol / errEmptyName 是注册期的参数错误。
var (
	errNilProtocol = fmt.Errorf("honeypot: 协议适配器不能为 nil")
	errEmptyName   = fmt.Errorf("honeypot: 协议名不能为空（名字是注册表的键，也是运行期的身份）")
)

// DuplicateError 表示协议名重复注册。
//
// 必须报错而不是覆盖：否则「哪个实现生效」取决于注册顺序 ——
// 那是排障时最难查的一类问题（配置说 ssh，跑起来是别的）。
type DuplicateError struct {
	Name string
}

func (e *DuplicateError) Error() string {
	return fmt.Sprintf("honeypot: 协议 %q 已注册过（名字必须唯一）", e.Name)
}

// notImplementedError 是「框架已就绪、核心逻辑待实现」的错误。
//
// 用错误类型而不是 error 变量：调用方可以用 errors.As 判断，
// 也让「没实现」与「实现了但失败」在日志里分得清。
type notImplementedError struct {
	what string
}

func (e notImplementedError) Error() string {
	return fmt.Sprintf("honeypot: %s 尚未实现（框架已就绪，核心逻辑待设计）", e.what)
}

// NotImplemented 返回一个「尚未实现」错误。
//
// ⚠️ **故意失败**：静默返回空结果会让人以为「功能在跑、只是没抓到东西」，
// 而真相是那段逻辑还没写。蜜罐面在请求路径之外，失败不影响业务（`NI-1`）。
func NotImplemented(what string) error {
	return notImplementedError{what: what}
}
