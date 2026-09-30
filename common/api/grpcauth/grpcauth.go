// Package grpcauth 给核心 gRPC 面（判定 / 策略 / 遥测）加**共享令牌**鉴权。
//
// 为什么需要它：核心的 gRPC 一贯只绑回环（明文 + assertPlaintextListenIsLocal 失败关闭），
// 本包是"跨节点部署"场景的最小防线 —— 与管控台 Bearer 令牌同语义：
// 客户端每个 RPC 携带 `x-shen-token` 元数据，服务端常数时间比较；不匹配即 Unauthenticated。
// 令牌为空 = 关闭（回环部署行为逐字节不变，存量测试不受影响）。
//
// 红线不变：本包不替代 mTLS —— 明文链路上的令牌可被窃听，跨信任边界仍必须 TLS/mTLS。
package grpcauth

import (
	"context"
	"crypto/subtle"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// MetadataKey 承载共享令牌的 gRPC 元数据键（metadata 键必须小写）。
const MetadataKey = "x-shen-token"

// ClientCredentials 返回随**每个 RPC** 发送令牌的 PerRPCCredentials。
// RequireTransportSecurity=false：允许在明文回环连接上使用（令牌校验由服务端完成）。
func ClientCredentials(token string) credentials.PerRPCCredentials {
	return tokenCreds{token: strings.TrimSpace(token)}
}

type tokenCreds struct{ token string }

func (c tokenCreds) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{MetadataKey: c.token}, nil
}

func (c tokenCreds) RequireTransportSecurity() bool { return false }

// ServerInterceptors 返回服务端校验拦截器（unary 与 stream 成对）。
// token 为空 ⇒ 两个返回值都是 nil，调用方不应装配（保持"未配置 = 关闭"的既有语义）。
func ServerInterceptors(token string) (grpc.UnaryServerInterceptor, grpc.StreamServerInterceptor) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, nil
	}
	unary := func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if err := authorize(ctx, token); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
	stream := func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if err := authorize(ss.Context(), token); err != nil {
			return err
		}
		return handler(srv, ss)
	}
	return unary, stream
}

// authorize 常数时间比较元数据里的令牌。错误一律 Unauthenticated（不区分缺头与错值，
// 避免给探测者提供"哪一步错了"的信号）。
func authorize(ctx context.Context, token string) error {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "缺少元数据")
	}
	vals := md.Get(MetadataKey)
	if len(vals) == 0 || subtle.ConstantTimeCompare([]byte(vals[0]), []byte(token)) != 1 {
		return status.Error(codes.Unauthenticated, "gRPC 令牌无效")
	}
	return nil
}
