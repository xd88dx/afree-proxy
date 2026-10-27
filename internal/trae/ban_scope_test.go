package trae

import "testing"

// 按端点封禁隔离（2026-10-27 用户定）：trae-b(traecode/raw) 的封禁不影响
// trae-a(remote) 的同 ID 模型；全部档位都被封禁才从 auto 列表消失。
func TestModelListScopedBan(t *testing.T) {
	resetCatalog(t)
	prev := Get()
	t.Cleanup(func() { Set(prev) })

	banned := map[string]bool{}
	SetBanChecker(func(scope, bareID string) bool { return banned[scope+"|"+bareID] })
	t.Cleanup(func() { SetBanChecker(nil) })

	Set(&Config{Gateway: DefaultGateway, Accounts: []*Account{{ID: "u1", Token: "tok"}}, Endpoint: "auto"})
	catalogMu.Lock()
	catalogModels = map[string]Model{
		// 三档全有：raw 封禁后仍可走 remote，auto 列表应保留
		"glm-5.3-flash": {ID: "glm-5.3-flash", Native: true, Function: "solo_agent",
			Tiers: []string{"traecode", "remote", "traework"}},
		// 仅原生档：raw 封禁 = 无可用传输，auto 列表应摘除
		"native-only": {ID: "native-only", Native: true, Function: "chat_v3", Tiers: []string{"traecode"}},
	}
	catalogLoaded = true
	catalogMu.Unlock()

	contains := func(id string) bool {
		for _, m := range ModelList() {
			if m.ID == id {
				return true
			}
		}
		return false
	}

	if !contains("glm-5.3-flash") || !contains("native-only") {
		t.Fatal("precondition: both listed before any ban")
	}

	// trae-b（traecode/raw）封禁：三档模型保留（remote/traework 仍可用），
	// 仅原生档模型摘除（无可用传输）。
	banned["traecode|glm-5.3-flash"] = true
	banned["traecode|native-only"] = true
	if !contains("glm-5.3-flash") {
		t.Fatal("traecode-scope ban must not hide a model with remote/traework tiers")
	}
	if contains("native-only") {
		t.Fatal("native-only model must vanish when its only tier is banned")
	}

	// 指定端点视图：remote 列表不受 traecode 封禁影响
	Set(&Config{Gateway: DefaultGateway, Accounts: []*Account{{ID: "u1", Token: "tok"}}, Endpoint: "remote"})
	if !contains("glm-5.3-flash") {
		t.Fatal("remote view must still list the model (traecode ban is scoped)")
	}
	// remote 档也封禁后消失
	banned["remote|glm-5.3-flash"] = true
	if contains("glm-5.3-flash") {
		t.Fatal("remote-scope ban must hide the model in the remote view")
	}

	// 全档封禁后 auto 列表摘除
	Set(&Config{Gateway: DefaultGateway, Accounts: []*Account{{ID: "u1", Token: "tok"}}, Endpoint: "auto"})
	banned["traework|glm-5.3-flash"] = true
	if contains("glm-5.3-flash") {
		t.Fatal("model banned on every tier must vanish from the auto list")
	}

	// 显式解析不受封禁影响（失败时给上游真实错误）
	if _, ok := Resolve("glm-5.3-flash"); !ok {
		t.Fatal("scoped bans must not break Resolve")
	}
}
