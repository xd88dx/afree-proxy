//go:build ignore

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
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
	admin := get(base + "/admin/")
	for _, marker := range []string{"Banned platforms", "Proxy exclusion rules", "accModelsBody", "ocModelsBody", "wbModelsBody", "data-wb-account", "platformBanBtn", "ppBulkBtn"} {
		fmt.Printf("admin %q: %d\n", marker, strings.Count(admin, marker))
	}
	fmt.Println("admin stale 'Free model catalog':", strings.Count(admin, "Free model catalog"))
	fmt.Println("admin stale 'modelsProbeInfo':", strings.Count(admin, "modelsProbeInfo"))

	zenCount := func(body string) int {
		var d struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		json.Unmarshal([]byte(body), &d)
		n := 0
		for _, m := range d.Data {
			if strings.HasPrefix(m.ID, "zen:") {
				n++
			}
		}
		return n
	}
	total := func(body string) int {
		var d struct {
			Data []json.RawMessage `json:"data"`
		}
		json.Unmarshal([]byte(body), &d)
		return len(d.Data)
	}
	m0 := get(base + "/v1/models")
	fmt.Println("models baseline total:", total(m0), "zen:", zenCount(m0))

	fmt.Println("ban zen:", post(base+"/admin/api/platforms/blacklist/ban", `{"platform":"zen"}`))
	m1 := get(base + "/v1/models")
	fmt.Println("models after ban total:", total(m1), "zen:", zenCount(m1))
	fmt.Println("gate zen chat:", post(base+"/v1/chat/completions", `{"model":"zen:big-pickle","messages":[{"role":"user","content":"hi"}]}`))
	fmt.Println("gate wb chat:", post(base+"/v1/chat/completions", `{"model":"wbcn:kimi-k2","messages":[{"role":"user","content":"hi"}]}`))
	fmt.Println("unban zen:", post(base+"/admin/api/platforms/blacklist/unban", `{"platform":"zen"}`))
	m2 := get(base + "/v1/models")
	fmt.Println("models after unban zen:", zenCount(m2))
	fmt.Println("ban list:", get(base+"/admin/api/platforms/blacklist"))
	fmt.Println("ban unknown:", post(base+"/admin/api/platforms/blacklist/ban", `{"platform":"nope"}`))
}
