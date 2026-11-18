module afree-proxy

go 1.26.0

require (
	github.com/andybalholm/brotli v1.0.6
	github.com/klauspost/compress v1.17.4
	github.com/redis/go-redis/v9 v9.22.0
	github.com/refraction-networking/utls v1.8.2
	golang.org/x/net v0.57.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/crypto v0.54.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
)

// vendored 上游树以独立 module 引入（module 路径与上游一致，import 零改写）；
// replace 指向本仓内目录，go.work 汇总三者便于本地 `go test ./...`。
// 门面是唯一消费入口：modules/*/afree_bridge.go（Go internal 规则天然禁止
// 主 module 直接 import vendor 的 internal/*）。
require (
	github.com/misakano7545/workbuddy2api-panel v0.0.0-00010101000000-000000000000
	trae2api v0.0.0-00010101000000-000000000000
)

replace github.com/misakano7545/workbuddy2api-panel => ./modules/workbuddy2api

replace trae2api => ./modules/trae2api
