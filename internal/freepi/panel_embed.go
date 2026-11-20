package freepi

import _ "embed"

// 面板片段（P5 拆分）：共享骨架在 internal/panel/html.go，本平台导航项与
// tab 片段经 embed 由 panel 按拆分前顺序拼装。片段与共享 JS 间的 id 契约
// （freepiKeysBody 等）——改 id 必须同步 panel/html.go 的 script 段。
var (
	//go:embed panel_nav.html
	PanelNavItem string
	//go:embed panel_tab.html
	PanelTabHTML string
)
