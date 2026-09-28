package rbac

import "testing"

// TestMatrix 逐格钉住授权矩阵：分离管理的约定（欺骗运维看不到蜜罐层、蜜罐运维不能改登记）
// 一旦被改动，这里会第一个失败。
func TestMatrix(t *testing.T) {
	cases := []struct {
		role Role
		perm Permission
		want bool
	}{
		{Admin, UsersAdmin, true},
		{Admin, AuditRead, true},
		{Admin, RegistryWrite, true},

		{DeceptionOperator, DeceptionRead, true},
		{DeceptionOperator, RegistryWrite, true},
		{DeceptionOperator, HoneypotRead, false},
		{DeceptionOperator, UsersAdmin, false},
		{DeceptionOperator, AuditRead, false},

		{HoneypotOperator, HoneypotRead, true},
		{HoneypotOperator, RegistryRead, true},
		{HoneypotOperator, DeceptionRead, false},
		{HoneypotOperator, RegistryWrite, false},
		{HoneypotOperator, UsersAdmin, false},

		{Viewer, OverviewRead, true},
		{Viewer, DeceptionRead, true},
		{Viewer, HoneypotRead, true},
		{Viewer, RegistryWrite, false},
		{Viewer, UsersAdmin, false},
		{Viewer, AuditRead, false},

		// 大模型：登记/密钥只给管理员；分析会计费，只读账号不给。
		{Admin, LLMAdmin, true},
		{Admin, LLMUse, true},
		{DeceptionOperator, LLMUse, true},
		{DeceptionOperator, LLMAdmin, false},
		{HoneypotOperator, LLMUse, true},
		{HoneypotOperator, LLMAdmin, false},
		{Viewer, LLMUse, false},
		{Viewer, LLMAdmin, false},

		{Role("root"), OverviewRead, false},
		{Admin, Permission("secret:read"), false},
	}
	for _, tc := range cases {
		if got := Can(tc.role, tc.perm); got != tc.want {
			t.Errorf("Can(%s, %s) = %v，期望 %v", tc.role, tc.perm, got, tc.want)
		}
	}
}

func TestEveryRoleCanManageSelfAndReadOverview(t *testing.T) {
	for _, r := range Roles() {
		if !Valid(r) {
			t.Fatalf("Roles() 返回了未登记角色 %s", r)
		}
		for _, p := range []Permission{SelfManage, OverviewRead, StreamRead} {
			if !Can(r, p) {
				t.Errorf("%s 缺少基础权限 %s", r, p)
			}
		}
	}
	if Valid(Role("")) {
		t.Error("空角色不应有效")
	}
}

func TestPermissionsOfSortedAndKnown(t *testing.T) {
	perms := PermissionsOf(Admin)
	for i := 1; i < len(perms); i++ {
		if perms[i-1] >= perms[i] {
			t.Fatalf("权限列表未排序：%v", perms)
		}
	}
	for _, p := range perms {
		if !Known(p) {
			t.Errorf("%s 应为已登记权限", p)
		}
	}
	if Known(Permission("nope")) {
		t.Error("未登记权限不应被认为已知")
	}
}
