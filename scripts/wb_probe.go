//go:build ignore

// wb_probe.go WorkBuddy 双开关端到端探针（一次性冒烟工具，不参与构建）：
// 用法：先准备种子部署（DATA_DIR/workbuddy/auths/u1.json + config.json 默认生成），
// 起服务后 `go run scripts/wb_probe.go <port>`。
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

func get(url string) string {
	r, err := http.Get(url)
	if err != nil {
		return "ERR: " + err.Error()
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	return string(b)
}

func post(url, body string) string {
	r, err := http.Post(url, "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		return "ERR: " + err.Error()
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	return fmt.Sprintf("%d %s", r.StatusCode, string(b))
}

func main() {
	base := "http://127.0.0.1:" + os.Args[1]
	ov := func() (accountEnabled, poolEnabled string, ok bool) {
		// vendor 面板 overview 是顶层 JSON（无 apiResponse.data 包裹）
		var d struct {
			Accounts []struct {
				UID            string `json:"uid"`
				AccountEnabled *bool  `json:"account_enabled"`
				PoolEnabled    bool   `json:"pool_enabled"`
			} `json:"accounts"`
		}
		raw := get(base + "/admin/workbuddy/api/overview")
		if json.Unmarshal([]byte(raw), &d) != nil {
			return "?raw=" + raw[:min(80, len(raw))], "?", false
		}
		for _, a := range d.Accounts {
			if a.UID == "u1" {
				ae := "nil(true)"
				if a.AccountEnabled != nil {
					ae = fmt.Sprintf("%v", *a.AccountEnabled)
				}
				return ae, fmt.Sprintf("%v", a.PoolEnabled), true
			}
		}
		return "?", "?", false
	}

	fmt.Println("account-enabled off:", post(base+"/admin/api/workbuddy/account-enabled", `{"uid":"u1","enabled":false}`))
	ae, pe, _ := ov()
	fmt.Println("overview after off: account_enabled=", ae, " pool_enabled=", pe)
	fmt.Println("interlock (pool on while account off):", post(base+"/admin/api/workbuddy/enabled", `{"uid":"u1","enabled":true}`))
	fmt.Println("account-enabled on:", post(base+"/admin/api/workbuddy/account-enabled", `{"uid":"u1","enabled":true}`))
	fmt.Println("pool on now ok:", post(base+"/admin/api/workbuddy/enabled", `{"uid":"u1","enabled":true}`))
	fmt.Println("pool off:", post(base+"/admin/api/workbuddy/enabled", `{"uid":"u1","enabled":false}`))
	ae, pe, _ = ov()
	fmt.Println("overview final: account_enabled=", ae, " pool_enabled=", pe)
	fmt.Println("unknown uid:", post(base+"/admin/api/workbuddy/account-enabled", `{"uid":"nope","enabled":true}`))
}
