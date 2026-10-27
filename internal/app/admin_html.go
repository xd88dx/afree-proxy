package app

const adminHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>AFree Proxy Admin</title>
<style>
:root{
  --bg:#0d0f12;--bg2:#131519;--bg3:#1a1d23;--panel:rgba(255,255,255,.028);
  --border:rgba(255,255,255,.08);--border-strong:rgba(255,255,255,.16);
  --text:#d6dae2;--text2:#8b919d;--text3:#5d626c;
  --accent:#8ab4f8;--accent2:#7ee2a8;--amber:#d9b04a;--danger:#e0705f;--pink:#f2a0b8;
  --accent-grad:linear-gradient(135deg,#8ab4f8,#a8c7fa);
  --glow:0 0 0 1px rgba(138,180,248,.2);
  --status-active-bg:rgba(126,226,168,.1);--status-cooldown-bg:rgba(217,176,74,.1);
  --status-expired-bg:rgba(224,112,95,.1);
  --btn-primary-bg:#2b4a77;--btn-primary-hover:#34578c;
  --btn-success-bg:#27513c;--btn-success-hover:#2f6249;
  --radius:10px;--radius-sm:7px;
}
[data-theme="light"]{
  --bg:#f6f7f8;--bg2:#ffffff;--bg3:#eef0f2;--panel:#ffffff;
  --border:rgba(15,18,22,.1);--border-strong:rgba(15,18,22,.22);
  --text:#1c1f24;--text2:#5c636e;--text3:#9aa0aa;
  --accent:#33629c;--accent2:#2b7a4b;--amber:#96700f;--danger:#b3442f;--pink:#b95c78;
  --accent-grad:linear-gradient(135deg,#33629c,#4a7ab5);
  --glow:0 0 0 1px rgba(51,98,156,.18);
  --status-active-bg:rgba(43,122,75,.1);--status-cooldown-bg:rgba(150,112,15,.1);
  --status-expired-bg:rgba(179,68,47,.08);
  --btn-primary-bg:#33629c;--btn-primary-hover:#2a5284;
  --btn-success-bg:#2b7a4b;--btn-success-hover:#246a40;
}
*{margin:0;padding:0;box-sizing:border-box}
/* color-scheme 让原生控件（select 弹出层/滚动条/复选框）跟随主题：
   深色主题下 select 弹层才会用深底浅字，否则浏览器默认浅底配白色文字几乎不可读 */
:root,[data-theme="dark"]{color-scheme:dark}
[data-theme="light"]{color-scheme:light}
select option{background:var(--bg2);color:var(--text)}
html{-webkit-text-size-adjust:100%}
body{font-family:'Inter','Segoe UI','PingFang SC','Microsoft YaHei',system-ui,sans-serif;background:var(--bg);color:var(--text);font-size:14px;line-height:1.55;min-height:100vh}
.mono,code{font-family:'JetBrains Mono','Cascadia Code','Fira Code',Consolas,monospace;font-size:12px}

/* ===== Layout ===== */
.layout{display:flex;min-height:100vh}
.sidebar{width:236px;background:var(--panel);backdrop-filter:blur(14px);border-right:1px solid var(--border);padding:18px 10px;flex-shrink:0;display:flex;flex-direction:column;position:sticky;top:0;height:100vh;overflow-y:auto}
.sidebar h1{font-size:15px;font-weight:700;padding:2px 10px 16px;border-bottom:1px solid var(--border);margin-bottom:10px;display:flex;align-items:center;gap:8px;letter-spacing:.02em}
.sidebar h1 .logo{width:28px;height:28px;border-radius:7px;background:var(--btn-primary-bg);display:inline-flex;align-items:center;justify-content:center;color:#fff;box-shadow:var(--glow)}
.sidebar h1 .logo svg{width:15px;height:15px}
.sidebar h1 .brand-name{color:var(--text)}
.sidebar h1 .theme-toggle{margin-left:auto;padding:4px 8px}
.sidebar h1 span{color:var(--accent)}
.nav-item{display:flex;align-items:center;gap:10px;padding:8px 12px;border-radius:8px;cursor:pointer;color:var(--text2);transition:.15s;font-size:13.5px;margin-bottom:1px;position:relative}
.nav-item .nav-ico{width:18px;height:18px;display:inline-flex;align-items:center;justify-content:center;flex-shrink:0}
.nav-item .nav-ico svg{width:16px;height:16px;stroke:currentColor;fill:none;stroke-width:1.7;stroke-linecap:round;stroke-linejoin:round}
.nav-item:hover{color:var(--text);background:rgba(255,255,255,.05)}
.nav-item.active{color:var(--text);background:rgba(255,255,255,.07);font-weight:600}
.nav-item.active::before{content:'';position:absolute;left:-10px;top:22%;bottom:22%;width:2px;border-radius:2px;background:var(--accent)}
.nav-item.active .nav-ico svg{stroke:var(--accent)}
.nav-group{font-size:10.5px;font-weight:600;letter-spacing:.12em;text-transform:uppercase;color:var(--text3);padding:14px 12px 5px;user-select:none}
.sidebar-footer{margin-top:auto;padding:12px 8px 4px;font-size:11.5px;color:var(--text3);border-top:1px solid var(--border)}
.sidebar-footer a{color:var(--accent);text-decoration:none}
.main{flex:1;padding:26px 34px 60px;min-width:0;max-width:1500px;margin:0 auto;width:100%}
h2{font-size:21px;margin-bottom:18px;font-weight:700;letter-spacing:.01em}

/* ===== Dashboard platform cards ===== */
/* 仪表盘卡片：每行最多 4 个（平台已增到 7 个，auto-fit 会在宽屏摊成一行） */
.dash-grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:14px}
.dash-card{background:var(--panel);border:1px solid var(--border);border-radius:var(--radius);padding:16px 18px;cursor:pointer;transition:border-color .2s,transform .2s}
.dash-card:hover{border-color:var(--accent);transform:translateY(-1px)}
.dash-name{font-size:13px;font-weight:600;color:var(--text2)}
.dash-stats{font-size:14px;color:var(--text);margin-top:8px;font-variant-numeric:tabular-nums}
/* 代理池全局策略：每行最多 4 张卡片（7 个上游，超出折行） */
.pp-grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:12px 16px}
.pp-card{background:rgba(255,255,255,.025);border:1px solid var(--border);border-radius:var(--radius);padding:12px 14px;display:flex;flex-direction:column;gap:7px;min-width:0}
.pp-card label{font-size:12px;font-weight:600;color:var(--text2)}
.pp-card select{width:100%}
.pp-card .hint{font-size:11.5px;margin:0;line-height:1.45}
.stat-grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:14px}
.stat-card{background:rgba(255,255,255,.025);border:1px solid var(--border);border-radius:var(--radius);padding:13px 16px}
.stat-name{font-size:12px;font-weight:600;color:var(--text2)}
.stat-line{font-size:13px;color:var(--text);margin-top:6px;font-variant-numeric:tabular-nums}
@keyframes rise{from{opacity:0;transform:translateY(10px)}to{opacity:1;transform:none}}

/* ===== Sections ===== */
.section{background:var(--panel);border:1px solid var(--border);border-radius:var(--radius);margin-bottom:22px;overflow:hidden;animation:rise .4s ease both}
.section-title{padding:13px 18px;border-bottom:1px solid var(--border);font-weight:600;font-size:14px;display:flex;align-items:center;gap:8px}
.section-title .sec-ico{width:17px;height:17px;display:inline-flex;align-items:center;justify-content:center;flex-shrink:0}
.section-title .sec-ico svg{width:15px;height:15px;stroke:var(--text2);fill:none;stroke-width:1.7;stroke-linecap:round;stroke-linejoin:round}
.section-body{padding:18px}

/* ===== Tables ===== */
.table-wrap{overflow-x:auto}

table{width:100%;border-collapse:collapse}
th,td{text-align:left;padding:10px 14px;border-bottom:1px solid var(--border);font-size:13px;white-space:nowrap}
th{color:var(--text3);font-weight:600;font-size:11.5px;text-transform:uppercase;letter-spacing:.06em}
tbody tr{transition:.12s}
tbody tr:hover{background:rgba(255,255,255,.035)}
tbody tr:last-child td{border-bottom:none}

/* ===== 账号列昵称（cline 账号表 + WorkBuddy 账号表共用）===== */
.nick-cell{min-width:0;max-width:260px}
.nick-wrap{display:flex;align-items:center;gap:6px;min-width:0}
.nick-name{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.nick-sub{font-size:10px;color:var(--text3);margin-top:2px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.nick-pen{flex:none;display:inline-flex;cursor:pointer;color:var(--text3);opacity:0;transition:.12s;border:none;background:none;padding:2px;line-height:0}
tr:hover .nick-pen{opacity:.85}
.nick-pen:hover{opacity:1;color:var(--text1)}
.nick-input{width:150px;font-size:12.5px;padding:3px 8px;border:1px solid var(--border);border-radius:6px;background:var(--bg,#14161a);color:var(--text1)}
.nick-ok,.nick-no{flex:none;cursor:pointer;border:none;background:none;color:var(--text3);font-size:13px;line-height:1;padding:2px}
.nick-ok:hover{color:var(--success,#4ade80)}
.nick-no:hover{color:var(--danger,#f87171)}

/* ===== Proxy exclusion rules（平台/子平台级黑名单 + 上限，卡片网格）===== */
.pr-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px}
@media(max-width:1180px){.pr-grid{grid-template-columns:1fr}}
.pr-card{position:relative;border:1px solid var(--border);border-radius:var(--radius-sm);background:var(--panel);padding:9px 11px 10px;transition:.12s}
.pr-card:hover{border-color:var(--border-strong)}
.pr-card-sub{border-left:2px solid var(--border-strong)}
.pr-card-head{display:flex;align-items:center;gap:10px;justify-content:space-between;flex-wrap:wrap}
.pr-name{font-size:13px;font-weight:600;color:var(--text);overflow:hidden;text-overflow:ellipsis;white-space:nowrap;min-width:60px}
.pr-ctl{display:inline-flex;align-items:center;gap:14px;flex-shrink:0}
.pr-capbox{display:inline-flex;align-items:center;gap:5px}
.pr-cap{width:46px;padding:3px 6px;border:1px solid var(--border);border-radius:6px;background:var(--bg2);color:var(--text);font:inherit;font-size:12.5px;text-align:center}
.pr-cap:focus{outline:none;border-color:var(--accent);box-shadow:var(--glow)}
.pr-unit{font-size:11px;color:var(--text3);white-space:nowrap}
.pr-switch{display:inline-flex;align-items:center;gap:6px;cursor:pointer;font-size:11.5px;color:var(--text2);white-space:nowrap;margin:0}
.pr-switch input{appearance:none;-webkit-appearance:none;width:30px;height:17px;border-radius:9px;background:var(--bg3);border:1px solid var(--border-strong);position:relative;cursor:pointer;transition:.15s;margin:0;flex-shrink:0}
.pr-switch input:checked{background:var(--btn-primary-bg);border-color:var(--btn-primary-bg)}
.pr-switch input::after{content:'';position:absolute;top:2px;left:2px;width:11px;height:11px;border-radius:50%;background:var(--text2);transition:.15s}
.pr-switch input:checked::after{left:15px;background:#fff}
.pr-tagbox{display:flex;flex-wrap:wrap;gap:5px;align-items:center;margin-top:8px;padding:5px 6px;border:1px solid var(--border);border-radius:6px;background:var(--bg2);min-height:33px;cursor:text;transition:.12s}
.pr-tagbox:hover{border-color:var(--border-strong)}
.pr-tagbox:focus-within{border-color:var(--accent);box-shadow:var(--glow)}
.pr-chip{display:inline-flex;align-items:center;gap:2px;padding:1px 2px 1px 9px;border-radius:999px;border:1px solid var(--border-strong);background:var(--bg3);font-size:11.5px;color:var(--text2);max-width:100%}
.pr-chip button{border:0;background:transparent;color:var(--text3);cursor:pointer;font-size:13px;line-height:1;padding:1px 4px;border-radius:50%}
.pr-chip button:hover{color:var(--danger);background:var(--panel)}
#platformBanChips .pr-chip{background:rgba(224,112,95,.14);border-color:rgba(224,112,95,.45);color:var(--danger)}
/* pr-chip-allex 批量追加框里「已在所有平台排除」的回显 chip：红色警示底
   （与网关设置封禁平台 chip 同款）+ 虚线边置灰与待追加选择区分开，
   × = 从所有平台批量取消（暂存，点保存才落盘） */
.pr-chip-sel,.pr-chip-allex{background:rgba(224,112,95,.14);border-color:rgba(224,112,95,.45);color:var(--danger)}
.pr-tagin{flex:1;min-width:100px;border:0;background:transparent;color:var(--text);font:inherit;font-size:12.5px;outline:none;padding:2px}
/* 只读点选框内的输入框不吃全局 input:focus 蓝圈（与 pr-tagbox:focus-within 辉光叠加成双圈） */
.pr-tagin:focus{box-shadow:none;border:0;outline:none}
.pr-menu{position:absolute;z-index:40;left:11px;right:11px;margin-top:5px;max-height:220px;overflow-y:auto;border:1px solid var(--border-strong);border-radius:8px;background:var(--bg2);box-shadow:0 12px 28px rgba(0,0,0,.3)}
.pr-opt{display:flex;justify-content:space-between;align-items:center;gap:10px;padding:6px 10px;font-size:12.5px;color:var(--text);cursor:pointer}
.pr-opt:hover{background:var(--bg3)}
.pr-opt-host{color:var(--text3);font-size:11px;flex-shrink:0}
.pr-opt-empty{padding:8px 10px;color:var(--text3);font-size:12px}
.pr-group{margin-top:10px;border:1px dashed var(--border-strong);border-radius:var(--radius-sm);padding:8px 10px 10px}
.pr-group-head{display:flex;align-items:center;gap:8px;cursor:pointer;user-select:none}
.pr-chev{width:13px;height:13px;stroke:var(--text2);fill:none;stroke-width:2;stroke-linecap:round;stroke-linejoin:round;transition:.15s;flex-shrink:0}
.pr-group.open .pr-chev{transform:rotate(90deg)}
.pr-group-name{font-size:13px;font-weight:600;color:var(--text)}
.pr-group-meta{font-size:11.5px;color:var(--text3)}
.pr-group-body{margin-top:9px;display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px}
@media(max-width:1180px){.pr-group-body{grid-template-columns:1fr}}
.pr-none{font-size:12px;color:var(--text3)}
/* pb-menu 点选式多选下拉（封禁平台/批量追加排除共用）。打开时由 JS 挂到
   body 用 fixed 定位（pbMenuToggle）——.section 有 overflow:hidden + rise 动画
   层叠上下文，菜单留在 section 内会被裁剪/盖住（跨 section 下拉的两次踩坑）。
   背景必须不透明（--bg2），透明会把下方内容透出来（--card 在主题里不存在）。*/
.pb-menu{position:fixed;z-index:900;max-height:300px;overflow-y:auto;border:1px solid var(--border-strong);border-radius:8px;background:var(--bg2);box-shadow:0 12px 28px rgba(0,0,0,.3)}
.pb-row{display:flex;gap:9px;align-items:center;padding:7px 11px;font-size:12.5px;color:var(--text);cursor:pointer}
.pb-row:hover{background:var(--bg3)}
.pb-row input{width:auto;flex-shrink:0}
.pb-row-sub{padding-left:26px;color:var(--text2)}
.pb-foot{display:flex;gap:8px;padding:8px 10px;border-top:1px solid var(--border);position:sticky;bottom:0;background:var(--bg2)}
/* pb-box 点选开启器（只点击不开菜单，不做文本输入）：不要 :focus-within 辉光，
   hover 提示即可——辉光+蓝圈是「输入异常」观感的来源 */
.pb-box{cursor:pointer}
.pb-box:focus-within{border-color:var(--border-strong);box-shadow:none}
.pb-box .pr-tagin{cursor:pointer}
/* ===== Status badges ===== */
.status{display:inline-flex;align-items:center;gap:6px;padding:3px 10px;border-radius:999px;font-size:12px;font-weight:600}
.status.active{background:var(--status-active-bg);color:var(--accent2)}
.status.cooldown{background:var(--status-cooldown-bg);color:var(--amber)}
.status.expired{background:var(--status-expired-bg);color:var(--danger)}
.status.pending{background:var(--bg2);color:var(--text2)}
.status-dot.pending{background:var(--text3)}
.status-dot{width:7px;height:7px;border-radius:50%;display:inline-block}
.status-dot.active{background:var(--accent2)}
.status-dot.cooldown{background:var(--amber)}
.status-dot.expired{background:var(--danger)}

/* ===== Buttons ===== */
.btn{display:inline-flex;align-items:center;justify-content:center;gap:6px;padding:7px 15px;border:1px solid var(--border);border-radius:var(--radius-sm);background:rgba(255,255,255,.04);color:var(--text);cursor:pointer;font-size:13px;transition:.15s;text-decoration:none;font-family:inherit;white-space:nowrap}
.btn:hover{background:rgba(255,255,255,.09);border-color:var(--border-strong)}
.btn:active{transform:none}
.btn svg{width:14px;height:14px;stroke:currentColor;fill:none;stroke-width:1.8;stroke-linecap:round;stroke-linejoin:round;flex-shrink:0}
.btn-primary{background:var(--btn-primary-bg);border-color:transparent;color:#fff;font-weight:600}
.btn-primary:hover{background:var(--btn-primary-hover)}
.btn-success{background:var(--btn-success-bg);border-color:transparent;color:#fff;font-weight:600}
.btn-success:hover{background:var(--btn-success-hover)}
.btn-danger{border-color:rgba(224,112,95,.4);color:var(--danger);background:transparent}
.btn-danger:hover{background:rgba(224,112,95,.1);border-color:var(--danger)}
.btn-sm{padding:3px 10px;font-size:12px;border-radius:6px}
.btn-static:hover,.btn-static:focus{background:rgba(255,255,255,.04);border-color:var(--border)}
.btn-static.btn-primary:hover,.btn-static.btn-primary:focus{background:var(--btn-primary-bg);border-color:transparent}
.btn-static.btn-warn:hover,.btn-static.btn-warn:focus{background:var(--amber);border-color:var(--amber)}
.btn-sm svg{width:13px;height:13px}

/* ===== Forms ===== */
input,textarea,select{width:100%;padding:9px 13px;background:rgba(0,0,0,.25);border:1px solid var(--border);border-radius:var(--radius-sm);color:var(--text);font-size:13px;font-family:inherit;transition:.15s}
[data-theme="light"] input,[data-theme="light"] textarea,[data-theme="light"] select{background:#fff}
input::placeholder,textarea::placeholder{color:var(--text3)}
input:focus,textarea:focus,select:focus{outline:none;border-color:var(--accent);box-shadow:0 0 0 3px rgba(138,180,248,.12)}
textarea{resize:vertical;min-height:84px;font-family:'JetBrains Mono','Cascadia Code',Consolas,monospace;font-size:12px}
select{cursor:pointer;appearance:none;background-image:linear-gradient(45deg,transparent 50%,var(--text2) 50%),linear-gradient(135deg,var(--text2) 50%,transparent 50%);background-position:calc(100% - 18px) 55%,calc(100% - 13px) 55%;background-size:5px 5px;background-repeat:no-repeat;padding-right:32px}
.form-row{display:flex;gap:14px;align-items:flex-end;margin-bottom:14px;flex-wrap:wrap}
/* textarea 默认 inline 布局的基线空隙会让它在 flex-end 行里比旁边的
   select 高出几像素 —— 改 block 贴齐字段底边 */
.field textarea{display:block}
.form-row .field{flex:1;min-width:180px}
.form-row .field label{display:block;font-size:12px;color:var(--text2);margin-bottom:6px;font-weight:500}
.form-actions{display:flex;gap:10px;margin-top:14px;flex-wrap:wrap}
/* 出口绑定联想下拉（combobox） */
.eg-input{width:170px;font-size:12px;padding:5px 8px;border-radius:7px;border:1px solid var(--border-strong);background:var(--bg3);color:var(--text);cursor:pointer}
.eg-wrap{position:relative;display:inline-block;vertical-align:middle}
.eg-wrap.has-badge .eg-input{padding-right:52px}
.eg-wrap .eg-input:focus ~ .eg-badge{display:none}
.eg-badge{position:absolute;right:6px;top:50%;transform:translateY(-50%);font-size:10px;line-height:1;color:var(--bg);background:var(--bg2);border:1px solid transparent;border-radius:6px;padding:2px 5px;pointer-events:none;font-variant-numeric:tabular-nums;white-space:nowrap;font-weight:600;width:34px;text-align:center;box-sizing:border-box}
/* 宽度基准 = 两位数色签的自然宽度（34px）：个位数居中补白，三位数（.w3，
   如"100%"）收紧左右内边距塞进同一宽度，各档宽度恒定不随位数抖动 */
.eg-badge.w3,.eg-rate.w3{padding-left:2px;padding-right:2px}
/* 握手延迟徽标（.hs）恒定 right:46px（在线率槽位左侧）——只有延迟没有
   在线率时在线率位置空着，延迟徽标不右移。 */
.eg-badge.hs{right:46px}
.eg-wrap.has-hs .eg-input{padding-right:96px}
/* 在线率五档底色：≥80 绿 / ≥60 蓝 / ≥40 粉 / ≥20 黄 / 以下红。深色主题用
   粉彩底配深字、浅色主题用深底配浅字——文字取 var(--bg) 双主题皆可读 */
.eg-badge.t-green,.eg-rate.t-green,.pp-rate.t-green{background:var(--accent2)}
.eg-badge.t-blue,.eg-rate.t-blue,.pp-rate.t-blue{background:var(--accent)}
.eg-badge.t-pink,.eg-rate.t-pink,.pp-rate.t-pink{background:var(--pink)}
.eg-badge.t-yellow,.eg-rate.t-yellow,.pp-rate.t-yellow{background:var(--amber)}
.eg-badge.t-red,.eg-rate.t-red,.pp-rate.t-red{background:var(--danger)}
/* 代理健康表在线率：与下拉框同源的档位色，但不 floated/定宽，直接居中内联 */
.pp-rate{display:inline-block;font-size:11px;font-weight:600;line-height:1;color:var(--bg);border-radius:6px;padding:2px 6px;box-sizing:border-box;font-variant-numeric:tabular-nums;min-width:34px;text-align:center}
.pp-rate.w3{padding-left:2px;padding-right:2px}
.eg-input:focus{outline:none;border-color:var(--accent)}
.eg-menu{position:absolute;z-index:1200;background:var(--bg2);border:1px solid var(--border-strong);border-radius:8px;box-shadow:0 8px 24px rgba(0,0,0,.35);max-height:260px;overflow-y:auto;min-width:220px}
.eg-item{position:relative;padding:6px 10px;font-size:12px;cursor:pointer;color:var(--text);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.eg-item:hover{background:rgba(138,180,248,.15)}
.eg-item.active{background:rgba(138,180,248,.22)}
/* 菜单项右侧双标签改绝对定位固定槽位：在线率恒定最右（right:6px），握手
   延迟（.hs-tag）恒定 right:46px——只有延迟时在线率位置空着，延迟不右移。
   原 float:right 布局下单徽标会贴到最右，槽位不固定。含标签的项由
   .has-rates 预留右侧空间，避免文字 ellipsis 压到标签下面。 */
.eg-rate{position:absolute;right:6px;top:50%;transform:translateY(-50%);color:var(--bg);font-variant-numeric:tabular-nums;font-size:11px;font-weight:600;line-height:1;border-radius:6px;padding:2px 6px;width:34px;text-align:center;box-sizing:border-box;white-space:nowrap}
.eg-rate.hs-tag{right:46px}
.eg-item.has-rates{padding-right:96px}
.eg-item.eg-hint{cursor:default;color:var(--text3)}
.eg-item.eg-hint:hover{background:none}
.flex{display:flex;align-items:center;gap:8px}
.text-right{text-align:right}
.inline-flex{display:inline-flex;align-items:center;gap:6px}
.justify-between{display:flex;justify-content:space-between;align-items:center;gap:10px;flex-wrap:wrap}
.hint{font-size:12px;color:var(--text2);margin-top:8px;line-height:1.6}
.hint strong{color:var(--text)}

/* ===== Dashboard: endpoints ===== */
.endpoint-list{display:flex;flex-direction:column;gap:8px}
.endpoint{display:flex;align-items:center;gap:12px;padding:9px 13px;background:rgba(255,255,255,.025);border:1px solid var(--border);border-radius:8px;flex-wrap:wrap}
.endpoint code{background:rgba(138,180,248,.08);border:1px solid rgba(138,180,248,.2);color:var(--accent);padding:3px 10px;border-radius:6px;font-size:11.5px;white-space:nowrap}
.endpoint span{font-size:12px;color:var(--text2);flex:1;min-width:160px}
.danger-zone{border-color:rgba(224,112,95,.28)}
.danger-zone .section-title{color:var(--danger)}
.auto-pill{font-size:11px;color:var(--text2);background:rgba(255,255,255,.05);border:1px solid var(--border);border-radius:999px;padding:2px 10px;white-space:nowrap}
.auto-pill.paused{color:var(--text3);border-color:var(--border)}

/* ===== Toast ===== */
/* 通知条：顶部居中悬浮，不顶格贴边，与页头留出呼吸空隙 */
.toast{position:fixed;top:18px;left:50%;padding:12px 22px;border-radius:12px;color:#fff;z-index:9999;opacity:0;transform:translate(-50%,-14px) scale(.97);transition:.3s cubic-bezier(.2,.9,.3,1.2);font-size:13px;max-width:520px;backdrop-filter:blur(12px);border:1px solid rgba(255,255,255,.14);box-shadow:0 12px 40px rgba(2,6,23,.5);white-space:pre-line;text-align:center}
.toast.show{opacity:1;transform:translate(-50%,0)}
.toast.success{background:#2b5a44}.toast.error{background:#7a3a30}
.toast.info{background:#31465e}
.toast.warning{background:#6e5619}
/* 拖拽排序：拖拽中的行半透明 + 顶部细高亮，落点感知 */
tr.row-dragging{opacity:.45;box-shadow:inset 0 2px 0 var(--accent)}
tr[draggable="true"]{cursor:grab}
tr[draggable="true"]:active{cursor:grabbing}

/* ===== Misc ===== */
.loading{display:inline-block;width:14px;height:14px;border:2px solid var(--text3);border-top-color:var(--accent);border-radius:50%;animation:spin .7s linear infinite;vertical-align:-2px}
@keyframes spin{to{transform:rotate(360deg)}}
.empty{padding:30px;text-align:center;color:var(--text2)}
.empty-state{padding:44px 20px;text-align:center;color:var(--text2)}
.empty-state .icon{font-size:40px;margin-bottom:10px;display:block;opacity:.8}
/* ===== Multi-select (Other models) ===== */
.multi-select{position:relative;max-width:100%}
.multi-select .ms-toggle{width:100%;text-align:left;background:var(--bg2,#131519);border:1px solid var(--border);border-radius:6px;color:var(--text);padding:8px 10px;cursor:pointer;font-size:13px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.multi-select.drop-up .ms-panel{top:auto;bottom:calc(100% + 4px)}
/* ---- 子平台管理弹窗：可拖动排序的行 ---- */
.mg-row{display:flex;align-items:center;gap:10px;padding:8px 10px;margin-bottom:6px;
  background:var(--bg2,#131519);border:1px solid var(--border);border-radius:8px;cursor:grab}
.mg-row.dragging{opacity:.45;cursor:grabbing}
.mg-row.dragover{border-color:var(--accent);box-shadow:0 0 0 1px var(--accent) inset}
.mg-handle{flex:0 0 auto;color:var(--muted-fg,#8b93a1);cursor:grab;font-size:13px;letter-spacing:-2px;user-select:none;padding:0 2px}
.mg-name{flex:0 0 auto;font-weight:600;max-width:180px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.mg-prefix{flex:0 0 auto;font-family:'JetBrains Mono',Consolas,monospace;font-size:12px;color:var(--accent)}
.mg-meta{flex:1 1 auto;min-width:0;font-size:11px;color:var(--muted-fg,#8b93a1);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.mg-row .btn{flex:0 0 auto}
.mg-empty{padding:14px;text-align:center;color:var(--muted-fg,#8b93a1);font-size:12px}
.multi-select .ms-panel{position:absolute;z-index:60;top:calc(100% + 4px);left:0;right:0;background:var(--bg2,#131519);border:1px solid var(--border-strong);;border-radius:8px;box-shadow:0 8px 24px rgba(0,0,0,.25);overflow:hidden}
.multi-select .ms-search{padding:8px 10px;border-bottom:1px solid var(--border)}
.multi-select .ms-search input{width:100%;background:transparent;border:none;outline:none;color:var(--text);font-size:13px}
.multi-select .ms-list{max-height:260px;overflow-y:auto;padding:6px 0}
.multi-select .ms-item{display:flex;gap:8px;align-items:center;padding:6px 12px;cursor:pointer;font-size:13px;white-space:nowrap}
.multi-select .ms-item:hover{background:var(--bg2)}
.multi-select .ms-item>input[type=checkbox]{width:auto;flex:0 0 auto;margin:0;padding:0;accent-color:var(--accent)}
.multi-select .ms-item span{flex:1 1 auto;min-width:0;overflow:hidden;text-overflow:ellipsis}
.multi-select .ms-del{flex:0 0 auto;margin-left:auto;background:none;border:none;color:var(--text3);font-size:15px;line-height:1;cursor:pointer;padding:2px 6px;border-radius:4px}
.multi-select .ms-del:hover{color:var(--danger);background:var(--danger-dim,rgba(224,112,95,.12))}
.multi-select .ms-empty{padding:10px 12px;color:var(--text2);font-size:12px}
dialog .field label{display:block;font-size:12px;color:var(--text2);margin-bottom:5px}
.key-display{background:rgba(0,0,0,.3);padding:9px 13px;border-radius:var(--radius-sm);border:1px solid var(--border);font-family:'JetBrains Mono','Cascadia Code',Consolas,monospace;font-size:12px;word-break:break-all;cursor:pointer;transition:.15s}
.key-display:hover{border-color:var(--accent)}
.model-tag{display:inline-block;padding:2px 9px;border-radius:6px;font-size:11px;background:rgba(255,255,255,.06);color:var(--text2);margin:2px;letter-spacing:.02em}
.model-tag.free{border:1px solid rgba(126,226,168,.35);color:var(--accent2);background:rgba(126,226,168,.07)}
.model-tag.context-1m{border:1px solid rgba(138,180,248,.45);color:var(--accent);background:rgba(138,180,248,.12)}
.theme-toggle{display:inline-flex;align-items:center;gap:5px;padding:4px 9px;border:1px solid var(--border);border-radius:7px;background:rgba(255,255,255,.04);color:var(--text2);cursor:pointer;font-size:12px;transition:.15s;font-family:inherit}
.theme-toggle:hover{color:var(--text);background:rgba(255,255,255,.09)}
[data-theme="dark"] .theme-toggle .light-label{display:none}
body:not([data-theme="dark"]) .theme-toggle .dark-label{display:none}
.probe-pill{font-size:11px;color:var(--text3)}

/* ===== WorkBuddy integrated console ===== */
.wb-toolbar{display:flex;gap:8px;flex-wrap:wrap;align-items:center}
.wb-switch{display:inline-flex;align-items:center;gap:8px;min-height:34px;font-size:13px;color:var(--text);cursor:pointer}
.wb-switch input{width:auto;padding:0;accent-color:var(--accent)}
/* 导入弹窗沿用启用状态的滑动开关（iOS 样式，选中紫色；与截图参考一致） */
.imp-keep-row{display:flex;align-items:center;justify-content:space-between;gap:10px;font-size:13px;color:var(--text);cursor:pointer;min-height:26px}
.imp-tgl{position:relative;display:inline-block;width:40px;height:22px;flex-shrink:0}
.imp-tgl input{position:absolute;opacity:0;width:0;height:0}
.imp-tgl i{position:absolute;inset:0;border-radius:999px;background:var(--border-strong);transition:background .15s}
.imp-tgl i::before{content:'';position:absolute;left:3px;top:3px;width:16px;height:16px;border-radius:50%;background:#fff;box-shadow:0 1px 2px rgba(0,0,0,.25);transition:transform .15s}
.imp-tgl input:checked+i{background:#5b5bd6}
.imp-tgl input:checked+i::before{transform:translateX(18px)}
.wb-config-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:22px 28px}
.wb-config-group{min-width:0}
.wb-config-group h3{font-size:13px;font-weight:650;color:var(--text);margin-bottom:12px;padding-bottom:8px;border-bottom:1px solid var(--border)}
.wb-field{display:grid;grid-template-columns:minmax(112px,.8fr) minmax(120px,1.2fr);gap:12px;align-items:center;margin:9px 0}
.wb-field label{font-size:12px;color:var(--text2)}
.wb-field input,.wb-field select{min-width:0}
.wb-account{display:flex;align-items:center;gap:7px;min-width:0;max-width:150px}
.wb-account-main{min-width:0;flex:1}
.wb-account-name{font-weight:600;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.wb-account-id{font-size:11px;color:var(--text3);font-family:'JetBrains Mono',Consolas,monospace;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.wb-usage{font-family:'JetBrains Mono',Consolas,monospace;font-size:11.5px;color:var(--text2)}
.wb-action-group{display:flex;gap:6px;justify-content:flex-end;flex-wrap:wrap}
dialog{position:fixed;inset:0;margin:auto;width:min(720px,calc(100vw - 32px));max-height:calc(100vh - 48px);overflow:auto;padding:0;border:1px solid var(--border-strong);border-radius:8px;background:var(--bg2);color:var(--text);box-shadow:0 24px 70px rgba(0,0,0,.48)}
dialog.wb-wide{width:min(1080px,calc(100vw - 32px))}
dialog::backdrop{background:rgba(0,0,0,.62);backdrop-filter:blur(3px)}
.toast[popover]{position:fixed;inset:18px auto auto 50%;margin:0;border:1px solid rgba(255,255,255,.14);color:#fff;pointer-events:none}
.wb-dialog-head{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:14px 18px;border-bottom:1px solid var(--border);font-weight:650}
.wb-dialog-body{padding:18px}
.wb-dialog-actions{display:flex;gap:8px;flex-wrap:wrap;margin-top:16px}
.wb-link-row{display:flex;gap:8px;margin-top:10px}
.wb-link-row input{min-width:0}
.wb-task-actions{display:flex;gap:8px;flex-wrap:wrap;justify-content:flex-end}
.wb-pk-card{border:1px solid var(--border);border-radius:8px;padding:14px;background:rgba(255,255,255,.025);margin-bottom:16px}
.wb-pk-head{display:flex;align-items:baseline;justify-content:space-between;gap:12px;flex-wrap:wrap}
.wb-pk-name{font-weight:650;font-size:15px}
.wb-pk-big{font-size:26px;font-weight:700;font-variant-numeric:tabular-nums;margin-top:10px}
.wb-pk-sub{font-size:12px;color:var(--text2);margin-top:2px}
.wb-pk-mix{display:flex;height:8px;overflow:hidden;border-radius:999px;background:var(--bg3);margin-top:12px}
.wb-pk-mix i{display:block;min-width:2px}
.wb-pk-legend{display:flex;gap:8px 14px;flex-wrap:wrap;margin-top:10px;font-size:12px;color:var(--text2)}
.wb-pk-legend span{display:inline-flex;align-items:center;gap:6px}
.wb-pk-legend i{width:8px;height:8px;border-radius:2px;flex:none}
.wb-todo-group{border:1px solid var(--border);border-radius:8px;padding:12px;margin:10px 0;background:rgba(255,255,255,.02)}
.wb-todo-title{display:flex;align-items:center;gap:8px;font-weight:650;cursor:pointer;list-style:none}
.wb-todo-title::-webkit-details-marker{display:none}
.wb-todo-title::before{content:'▶';font-size:10px;color:var(--text3);transition:transform .15s}
.wb-todo-group[open] .wb-todo-title::before{transform:rotate(90deg)}
.wb-todo-title .auto-pill{margin-left:auto}
.wb-todo-row{display:flex;align-items:flex-start;gap:10px;padding:7px 0;border-top:1px solid var(--border)}
.wb-todo-row:first-of-type{border-top:0}
.wb-todo-row .nm{flex:1;min-width:0}
.wb-todo-row .id{font-size:11px;color:var(--text3);font-family:'JetBrains Mono',Consolas,monospace}

/* ===== Responsive ===== */
@media (max-width:980px){
  .layout{flex-direction:column}
  .sidebar{width:100%;height:auto;position:sticky;top:0;flex-direction:row;align-items:center;padding:10px 12px;overflow-x:auto;gap:4px;z-index:50}
  .sidebar h1{display:flex;align-items:center;border-bottom:none;margin:0;padding:0 6px 0 0;gap:6px;font-size:13px;white-space:nowrap}
  .sidebar h1 .brand-name{display:none}
  .sidebar .nav-item{padding:7px 11px;white-space:nowrap}
  .sidebar .nav-item.active::before{display:none}
  .nav-group{display:none}
  .sidebar .theme-toggle{margin-left:4px}
  .sidebar-footer{display:none}
  .main{padding:20px 16px 48px}
  .dash-grid,.stat-grid,.pp-grid{grid-template-columns:repeat(2,minmax(0,1fr))}
}
@media (max-width:560px){
  h2{font-size:18px}
  .dash-grid,.stat-grid,.pp-grid{grid-template-columns:1fr}
  .section-title{padding:11px 14px}
  .section-body{padding:14px}
  .form-row .field{min-width:100%}
  .wb-config-grid{grid-template-columns:1fr}
  .wb-field{grid-template-columns:1fr;gap:5px}
  .wb-toolbar{width:100%;justify-content:flex-start}
}
/* 账号表暂存编辑：有未保存修改的行高亮，对应平台的"保存"按钮加描边提醒 */
.tr-dirty > td{background:rgba(234,179,8,.09)}
.dirty-hint{box-shadow:0 0 0 2px var(--accent)}
.btn-warn{background:var(--amber);border-color:var(--amber);color:#1a1a1a;font-weight:600}
.trae-acc{border:1px solid var(--border);border-radius:10px;margin-bottom:10px;background:rgba(148,163,184,.04)}
.trae-acc summary{cursor:pointer;padding:10px 14px;font-weight:600;user-select:none;list-style:none;display:flex;align-items:center;gap:8px}
.trae-acc summary::-webkit-details-marker{display:none}
.trae-acc summary::before{content:'B8';color:var(--text3);transition:transform .15s;display:inline-block}
.trae-acc[open] summary::before{transform:rotate(90deg)}
.trae-acc-body{padding:0 14px 12px}
.btn-warn:hover{filter:brightness(1.1);background:var(--amber);border-color:var(--amber)}
</style>
</head>
<body>
<div class="layout">
<div class="sidebar">
<h1><span class="logo"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M13 2 3 14h7l-1 8 10-12h-7l1-8z"/></svg></span><span class="brand-name">AFree Proxy</span><button class="theme-toggle" id="langBtn" onclick="toggleLang()" title="Switch language / 切换语言" style="margin-left:6px"></button><button class="theme-toggle" onclick="toggleTheme()" title="Toggle theme"><span class="icon" id="themeIcon"></span><span class="light-label">Light</span><span class="dark-label">Dark</span></button></h1>
<div class="nav-item active" data-tab="dashboard"><span class="nav-ico"><svg viewBox="0 0 24 24"><rect x="3" y="3" width="7" height="9" rx="1.5"/><rect x="14" y="3" width="7" height="5" rx="1.5"/><rect x="14" y="12" width="7" height="9" rx="1.5"/><rect x="3" y="16" width="7" height="5" rx="1.5"/></svg></span> Dashboard</div>
<div class="nav-group">Account pool</div>
<div class="nav-item" data-tab="accounts"><span class="nav-ico"><svg viewBox="0 0 24 24"><circle cx="12" cy="8" r="4"/><path d="M4 21c0-4 3.6-6.5 8-6.5s8 2.5 8 6.5"/></svg></span> Cline</div>
<div class="nav-item" data-tab="trae"><span class="nav-ico"><svg viewBox="0 0 24 24"><path d="M5 5h14M12 5v14"/><path d="M8.5 12H12"/></svg></span> TraeWork</div>
<div class="nav-item" data-tab="workbuddy"><span class="nav-ico"><svg viewBox="0 0 24 24"><path d="M12 3l8 4.5v9L12 21l-8-4.5v-9L12 3z"/><path d="M12 12l8-4.5M12 12v9M12 12L4 7.5"/></svg></span> WorkBuddy</div>
<div class="nav-item" data-tab="opencode"><span class="nav-ico"><svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a14.5 14.5 0 0 1 0 18 14.5 14.5 0 0 1 0-18z"/></svg></span> OpenCode</div>
<div class="nav-item" data-tab="amdcloud"><span class="nav-ico"><svg viewBox="0 0 24 24"><rect x="3" y="5" width="13" height="13" rx="2"/><path d="M16 9h3l2 3-2 3h-3"/><circle cx="9.5" cy="11.5" r="2.5"/></svg></span> AMDCloud</div>
<div class="nav-item" data-tab="openrouter"><span class="nav-ico"><svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="9"/><path d="M8 12h8M12 8v8"/><circle cx="12" cy="12" r="2.5"/></svg></span> OpenRouter</div>
<div class="nav-item" data-tab="tokenharbor"><span class="nav-ico"><svg viewBox="0 0 24 24"><path d="M4 21V9l8-6 8 6v12"/><path d="M4 21h16"/><path d="M9 21v-6h6v6"/></svg></span> TokenHarbor</div>
<div class="nav-item" data-tab="other"><span class="nav-ico"><svg viewBox="0 0 24 24"><rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><circle cx="17.5" cy="17.5" r="3.5"/></svg></span> Other</div>
<div class="nav-group">Services</div>
<div class="nav-item" data-tab="proxypool"><span class="nav-ico"><svg viewBox="0 0 24 24"><circle cx="12" cy="5" r="2.5"/><circle cx="5" cy="19" r="2.5"/><circle cx="19" cy="19" r="2.5"/><path d="M12 7.5v4m0 0-5.5 5m5.5-5 5.5 5"/></svg></span> Proxy pool</div>
<div class="nav-item" data-tab="settings"><span class="nav-ico"><svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .34 1.87l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.7 1.7 0 0 0-1.87-.34 1.7 1.7 0 0 0-1 1.55V21a2 2 0 1 1-4 0v-.09a1.7 1.7 0 0 0-1-1.55 1.7 1.7 0 0 0-1.87.34l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.7 1.7 0 0 0 .34-1.87 1.7 1.7 0 0 0-1.55-1H3a2 2 0 1 1 0-4h.09a1.7 1.7 0 0 0 1.55-1 1.7 1.7 0 0 0-.34-1.87l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.7 1.7 0 0 0 1.87.34h0a1.7 1.7 0 0 0 1-1.55V3a2 2 0 1 1 4 0v.09a1.7 1.7 0 0 0 1 1.55h0a1.7 1.7 0 0 0 1.87-.34l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.7 1.7 0 0 0-.34 1.87v0a1.7 1.7 0 0 0 1.55 1H21a2 2 0 1 1 0 4h-.09a1.7 1.7 0 0 0-1.55 1z"/></svg></span> Gateway settings</div>
<div class="nav-item" data-tab="combos"><span class="nav-ico"><svg viewBox="0 0 24 24"><path d="M8 3v5a4 4 0 0 1-4 4 4 4 0 0 1 4 4v5M16 3v5a4 4 0 0 0 4 4 4 4 0 0 0-4 4v5"/></svg></span> Custom aliases</div>
<div class="nav-item" data-tab="logs"><span class="nav-ico"><svg viewBox="0 0 24 24"><path d="M8 6h13M8 12h13M8 18h13"/><path d="M3 6h.01M3 12h.01M3 18h.01"/></svg></span> Request logs</div>
<div class="sidebar-footer">
  <div>Admin panel: <a href="/admin/">/admin/</a></div>
  <div>API address: <span id="footerApiAddr">http://127.0.0.1:3457</span></div>
</div>
</div>

<div class="main">

<div id="tab-dashboard" class="tab-panel">
<h2>Dashboard</h2>
<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/></svg></span> Platform overview</div>
  <div class="section-body">
    <div class="dash-grid">
      <div class="dash-card" onclick="switchTab('accounts')">
        <div class="dash-name">Cline</div>
        <div class="dash-stats" id="dashCline">-</div>
      </div>
      <div class="dash-card" onclick="switchTab('trae')">
        <div class="dash-name">TraeWork</div>
        <div class="dash-stats" id="dashTrae">-</div>
      </div>
      <div class="dash-card" onclick="switchTab('workbuddy')">
        <div class="dash-name">WorkBuddy</div>
        <div class="dash-stats" id="dashWb">-</div>
      </div>
      <div class="dash-card" onclick="switchTab('opencode')">
        <div class="dash-name">OpenCode</div>
        <div class="dash-stats" id="dashOc">-</div>
      </div>
      <div class="dash-card" onclick="switchTab('amdcloud')">
        <div class="dash-name">AMDCloud</div>
        <div class="dash-stats" id="dashAmd">-</div>
      </div>
      <div class="dash-card" onclick="switchTab('openrouter')">
        <div class="dash-name">OpenRouter</div>
        <div class="dash-stats" id="dashOr">-</div>
      </div>
      <div class="dash-card" onclick="switchTab('tokenharbor')">
        <div class="dash-name">TokenHarbor</div>
        <div class="dash-stats" id="dashTh">-</div>
      </div>
      <div class="dash-card" onclick="switchTab('other')">
        <div class="dash-name">Other</div>
        <div class="dash-stats" id="dashOther">-</div>
      </div>
    </div>
  </div>
</div>
<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M4 20v-8m5 8V8m5 12v-5m5 5V4"/><path d="M3 21h18"/></svg></span> Request statistics</div>
  <div class="section-body">
    <div class="stat-grid">
      <div class="stat-card"><div class="stat-name">Cline</div><div class="stat-line" id="reqCline">-</div></div>
      <div class="stat-card"><div class="stat-name">TraeWork</div><div class="stat-line" id="reqTrae">-</div></div>
      <div class="stat-card"><div class="stat-name">WorkBuddy</div><div class="stat-line" id="reqWb">-</div></div>
      <div class="stat-card"><div class="stat-name">OpenCode</div><div class="stat-line" id="reqZen">-</div></div>
      <div class="stat-card"><div class="stat-name">AMDCloud</div><div class="stat-line" id="reqAmd">-</div></div>
      <div class="stat-card"><div class="stat-name">OpenRouter</div><div class="stat-line" id="reqOr">-</div></div>
      <div class="stat-card"><div class="stat-name">TokenHarbor</div><div class="stat-line" id="reqTh">-</div></div>
      <div class="stat-card"><div class="stat-name">Other</div><div class="stat-line" id="reqOther">-</div></div>
    </div>
    <div class="hint" style="margin:10px 0 0" id="reqStatsHint">Counted since process start.</div>
  </div>
</div>
<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M9 12h6M12 9v6"/><circle cx="12" cy="12" r="9"/></svg></span> Endpoints</div>
  <div class="section-body">
    <div class="endpoint-list">
      <div class="endpoint"><code>POST /v1/chat/completions</code><span>OpenAI Chat format — works with most tools out of the box</span></div>
      <div class="endpoint"><code>POST /v1/messages</code><span>Anthropic Messages format — for Claude Code / Cline and similar clients</span></div>
      <div class="endpoint"><code>POST /v1/responses</code><span>OpenAI Responses format — for Cursor and similar clients</span></div>
      <div class="endpoint"><code>GET /v1/models</code><span>Model list</span></div>
    </div>
  </div>
</div>
</div>

<div id="tab-accounts" class="tab-panel" style="display:none">
  <div class="flex justify-between" style="margin-bottom:16px">
  <div>
    <h2 style="margin-bottom:4px">Cline</h2>
    <div class="hint" id="accSummary" style="margin:0">Loading...</div>
  </div>
</div>
<div class="section">
  <div class="section-body" style="padding:6px">
    <div class="table-wrap">
    <table>
      <thead>
        <tr><th style="width:44px">#</th><th>Email</th><th>Status</th><th title="Counted locally by this proxy — not the official free quota">Today/Total tokens</th><th>Last used</th><th title="Main exit first, backup as fallback; both down = row skipped (isolation).">Proxy binding</th><th style="text-align:center">Actions</th><th style="text-align:center" title="Routing participation. New rows start disabled — tick Pool to enable.">Pool</th></tr>
      </thead>
      <tbody id="accountTableBody">
        <tr><td colspan="8" class="empty">Loading...</td></tr>
      </tbody>
    </table>
    </div>
    <div style="display:flex;justify-content:space-between;align-items:center;gap:8px;margin:10px 6px 4px">
      <button class="btn btn-primary btn-sm" onclick="openAccAddDialog()">Add</button>
      <div style="display:flex;gap:8px">
        <button class="btn btn-sm" onclick="stageAllAccountsPool(true)">Enable all</button>
        <button class="btn btn-sm" onclick="stageAllAccountsPool(false)">Disable all</button>
        <button class="btn btn-sm" onclick="stageProxiesByOrder('acc')" title="Bind main proxy by row order: 1st credential to Direct, then the proxy pool in order (staged - click Save to apply; enable/disable and backup untouched)">Bind proxies in order</button>
        <button class="btn btn-sm" onclick="stageClearAccountProxies()" title="Stage only — click Save to apply. Clears bindings and disables every row.">Clear bindings</button>
        <button class="btn btn-sm" id="accTestBtn" onclick="batchTestAccounts()" title="Run the Test probe on every row.">Test all</button>
      </div>
      <div style="display:flex;gap:8px">
        <button class="btn btn-sm btn-warn" id="accResetBtn" onclick="resetAccountEdits()" title="Discard staged changes and reload the last saved state">Discard</button>
        <button class="btn btn-primary btn-sm" id="accSaveBtn" onclick="saveAccountEdits()">Save</button>
      </div>
    </div>
  </div>
</div>
<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M4 7h16M4 12h16M4 17h10"/></svg></span> Request headers (mimicking the Cline CLI)<button class="btn btn-sm" onclick="resetHeaders()" style="margin-left:auto" title="Restore the default Cline CLI headers">Restore defaults</button></div>
  <div class="section-body">
    <div class="table-wrap">
    <table>
      <thead><tr><th style="width:220px">Header</th><th>Value</th><th style="width:40px"></th></tr></thead>
      <tbody id="headersTableBody">
        <tr><td colspan="3" class="empty">Loading...</td></tr>
      </tbody>
    </table>
    </div>
    <div class="form-actions">
      <button class="btn btn-sm" onclick="addHeaderRow()">Add header</button>
      <button class="btn btn-sm btn-primary" onclick="saveHeaders()">Save headers</button>
    </div>
    <div class="hint">Attached to every Cline API request to mimic the official client.</div>
  </div>
</div>

<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M3 3v18h18"/><path d="M7 15l4-4 3 3 5-6"/></svg></span> Model catalog <span class="probe-pill" data-models-info style="font-weight:normal"></span></div>
  <div class="section-body">
    <div class="flex" style="margin-bottom:10px;gap:10px">
      <button class="btn btn-sm btn-primary" onclick="refreshModels()">Refresh models</button>
      <span class="hint" style="margin:0">Automatically synced from the official model source upstream (every 5 minutes)</span>
    </div>
    <div id="accModelsBody">Loading...</div>
  </div>
</div>
</div>

<div id="tab-trae" class="tab-panel" style="display:none">
<h2 style="margin-bottom:4px">TraeWork</h2>
<div class="hint" id="traeSummary" style="margin:0 0 16px">Loading...</div>

<div class="section">
  <div class="section-body">
    <div style="display:flex;justify-content:space-between;align-items:center;gap:8px;margin:0 6px 12px">
      <span class="wb-toolbar">
        <button class="btn btn-sm" onclick="refreshTraeCredits()" title="Refresh every enabled account's credit snapshot (feeds the Credits column and credit-priority selection)">Refresh balances</button>
      </span>
      <span class="wb-toolbar">
        <button class="btn btn-sm" onclick="runTraeCheckin()" title="Daily check-in for every enabled account that has not checked in today">Check in all</button>
      </span>
    </div>
    <div class="form-row">
      <div class="field" style="margin:0">
        <label>Gateway</label>
        <div style="display:flex;gap:8px;align-items:center">
          <input type="text" id="traeGateway" placeholder="https://trae-api-cn.mchost.guru" style="flex:1;min-width:0">
          <button class="btn btn-primary btn-sm" onclick="saveTraeConfig()">Confirm</button>
        </div>
      </div>
    </div>
    <div class="flex" style="gap:10px;margin:8px 0;align-items:center;flex-wrap:wrap">
      <label class="hint" style="margin:0">Probe model (used by the Test buttons):</label>
      <select id="traeProbeModel" style="max-width:420px"><option value="">auto</option></select>
      <button class="btn btn-sm" onclick="refreshTraeModels()" title="Re-sync the model catalog from the Trae upstream using an enabled account">Refresh models</button>
      <span class="hint" style="margin:0" id="traeModelsCount"></span>
    </div>
    <div class="table-wrap" style="margin-bottom:10px">
      <table>
        <thead><tr><th style="width:44px">#</th><th style="width:150px">Account</th><th>Status</th><th title="Remaining credits from the latest snapshot (auto-refreshed in the background; Test refreshes on demand)">Credits</th><th title="Counted locally by this proxy from upstream usage events">Today/Total tokens</th><th>Last used</th><th title="Main exit first, backup as fallback; both down = row skipped (isolation).">Proxy binding</th><th style="width:130px;text-align:center">Actions</th><th style="text-align:center" title="Gateway (reverse proxy) switch: unchecked = model sessions never route to this account; check-in and all other maintenance tasks are unaffected. Can only be ticked while the account switch is on.">Gateway switch</th><th style="text-align:center" title="Account master switch. Off = the account does not exist: check-in, balance refresh and Test all skip it, and it never routes.">Account switch</th></tr></thead>
        <tbody id="traeAccountsBody"><tr><td colspan="10" class="empty">Loading...</td></tr></tbody>
      </table>
    </div>
    <div style="display:flex;justify-content:space-between;align-items:center;gap:8px;margin:10px 6px 4px">
      <button class="btn btn-primary btn-sm" onclick="openTraeAddDialog()">Add accounts</button>
      <div style="display:flex;gap:8px">
        <button class="btn btn-sm" onclick="stageAllTraeRouting(true)">Enable all</button>
        <button class="btn btn-sm" onclick="stageAllTraeRouting(false)">Disable all</button>
        <button class="btn btn-sm" onclick="stageProxiesByOrder('trae')" title="Bind main proxy by row order: 1st account to Direct, then the proxy pool in order (staged - click Save to apply; enable/disable and backup untouched)">Bind proxies in order</button>
        <button class="btn btn-sm" onclick="stageClearTraeProxies()" title="Stage only — click Save to apply. Clears bindings and disables every row.">Clear bindings</button>
        <button class="btn btn-sm" id="traeTestBtn" onclick="batchTestTraeAccounts()" title="Run the Test probe on every row.">Test all</button>
      </div>
      <div style="display:flex;gap:8px">
        <button class="btn btn-sm btn-warn" id="traeResetBtn" onclick="resetTraeEdits()" title="Discard staged changes and reload the last saved state">Discard</button>
        <button class="btn btn-primary btn-sm" id="traeSaveBtn" onclick="saveTraeEdits()">Save</button>
      </div>
    </div>
  </div>
</div>

<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg></span> Rate-limit defense</div>
  <div class="section-body">
    <div class="form-row">
      <div class="field"><label>Max concurrency</label><input type="text" id="traeMaxConc" placeholder="8"></div>
      <div class="field"><label>Rate-limit retries</label><input type="text" id="traeRetries" placeholder="3"></div>
    </div>
    <div class="form-actions"><button class="btn btn-primary" onclick="saveTraeConfig()">Save rate-limit config</button></div>
  </div>
</div>

<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M12 6v6l4 2"/><circle cx="12" cy="12" r="9"/></svg></span> Automation</div>
  <div class="section-body">
    <div class="form-row">
      <div class="field">
        <label>Selection strategy</label>
        <select id="traeSelection">
          <option value="">Round-robin</option>
          <option value="credit-priority">Credit priority (richest first)</option>
        </select>
      </div>
      <div class="field" style="display:flex;align-items:flex-end">
        <label class="hint" style="display:flex;gap:6px;align-items:center;margin:0 0 8px;cursor:pointer;white-space:nowrap">
          <input type="checkbox" id="traeAutoCheckin"> Daily check-in
        </label>
      </div>
    </div>
    <div class="hint" style="margin-top:0">Credit priority picks the account with the most remaining credits first (unlimited accounts always first; needs the background balance refresh). Daily check-in claims every enabled account once per day.</div>
    <div class="form-actions"><button class="btn btn-primary" onclick="saveTraeConfig()">Save automation config</button></div>
  </div>
</div>

<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M3 3v18h18"/><path d="M7 15l4-4 3 3 5-6"/></svg></span> Model catalog</div>
  <div class="section-body">
    <div class="flex" style="margin-bottom:10px;gap:10px;align-items:center;flex-wrap:wrap">
      <button class="btn btn-sm btn-primary" onclick="refreshTraeModels()">Refresh models</button>
          <span class="hint" style="margin:0;flex:1;min-width:200px">Automatically synced from the official model source upstream (every 5 minutes)</span>
      <label class="hint" style="margin:0;white-space:nowrap" for="traeEndpoint">Endpoint</label>
      <select id="traeEndpoint" onchange="saveTraeEndpoint(this.value)" style="max-width:220px" title="Upstream endpoint: Auto = merged catalog via the native raw_chat transport; Remote / TraeWork switch requests to the chat_sessions transport (same as the upstream project)">
        <option value="auto">Auto route</option>
        <option value="remote">Remote</option>
        <option value="traecode">TraeCode</option>
        <option value="traework">TraeWork</option>
      </select>
    </div>
    <div id="traeModelsBody">Loading...</div>
    </div>
  </div>
</div>

<div id="tab-proxypool" class="tab-panel" style="display:none">
<h2>Proxy pool</h2>
<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><circle cx="12" cy="5" r="2.5"/><circle cx="5" cy="19" r="2.5"/><circle cx="19" cy="19" r="2.5"/><path d="M12 7.5v4m0 0-5.5 5m5.5-5 5.5 5"/></svg></span> Egress proxies</div>
  <div class="section-body">
    <div class="hint" style="margin-bottom:10px">One proxy per line: <code>http://user:pass@host:port</code> or <code>socks5://host:port#alias</code>. All upstreams share this list; unhealthy proxies are skipped automatically.</div>
    <div class="hint" style="margin-bottom:10px">The alias (#name) is a proxy's identity — re-binding survives alias-preserving URL changes.</div>
    <div class="hint" style="margin-bottom:10px">When a subscription URL is set, this list is managed by the subscription and read-only; it serves as the fallback pool while a fetch fails.</div>
    <div class="form-row">
      <div class="field" style="flex:3"><label>Proxy list</label>
        <textarea id="ppProxies" rows="4" placeholder="one per line: socks://b64(user:pass)@host:port#alias, socks5://user:pass@host:port#alias or http://host:port"></textarea>
      </div>
      <div class="field" style="flex:1"><label>Proxy selection strategy</label>
        <select id="ppStrategy"><option value="round_robin">Round-robin (round_robin)</option><option value="random">Random (random)</option><option value="fill">Fill (fill)</option></select>
      </div>
    </div>
    <div class="form-row">
      <div class="field" style="flex:3"><label>Subscription URL</label>
        <input id="ppSubURL" oninput="syncProxyListEditable()" placeholder="http://10.10.10.5:12321/proxy/socks.txt — the pool is then managed by this subscription; leave empty to edit the list manually">
      </div>
      <div class="field" style="flex:1"><label>Update frequency (cron)</label>
        <div style="display:flex;gap:8px;align-items:center">
          <input id="ppSubCron" placeholder="*/30 * * * * (min hour day month weekday)" style="flex:1;min-width:0">
          <button class="btn" id="ppSubUpdateBtn" onclick="triggerProxySubUpdate()" style="white-space:nowrap">Update now</button>
        </div>
      </div>
    </div>
    <div id="ppSubUpdateResult" style="margin:-6px 0 10px;font-size:12px;min-height:14px"></div>
    <div class="hint" style="margin-bottom:10px">Subscription refresh replaces the whole pool with the fetched list. Fetch failures keep the current pool.</div>
    <div class="form-actions"><button class="btn btn-primary" onclick="saveProxyPool()">Save proxy pool</button></div>
  </div>
</div>
<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M4 5h16M4 12h10M4 19h7"/></svg></span> Proxy exclusion rules</div>
  <div class="section-body">
    <div class="hint" style="margin-bottom:14px">Platform-level blacklist and per-proxy binding cap for automatic assignment only — one-click bind and reorder-by-latency. Manual bindings and runtime routing are never restricted.</div>
    <div style="margin-bottom:12px">
      <div style="display:flex;gap:10px;align-items:flex-start">
        <div style="flex:1;position:relative">
          <div class="pr-tagbox" id="ppBulkBox" style="margin-top:0;min-height:38px" onclick="ppBulkBoxClick(event)">
            <span id="ppBulkChips" style="display:contents"></span>
            <input class="pr-tagin" id="ppBulkIn" placeholder="Search and add a proxy…" onfocus="ppBulkFocus()" oninput="ppBulkInput(this.value)" onkeydown="ppBulkKey(event)">
          </div>
          <div id="ppBulkMenu" class="pb-menu" style="display:none"></div>
        </div>
        <button class="btn btn-sm btn-primary" style="margin-top:4px;white-space:nowrap" onclick="ppBulkApply()">Batch-append</button>
      </div>
    </div>
    <div id="ppRulesBody"><div class="pr-none" style="padding:10px 12px">Loading...</div></div>
    <div class="form-actions" style="margin-top:12px">
      <button class="btn btn-primary" onclick="saveProxyRules()">Save proxy exclusion rules</button>
      <button class="btn btn-success" onclick="openReorderDlg()" style="margin-left:10px">Reorder credential proxies by latency</button>
    </div>
  </div>
</div>
<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M12 5v14M5 12h14"/></svg></span> Global policy</div>
  <div class="section-body">
    <div class="pp-grid">
      <div class="pp-card">
        <label>Cline</label>
        <select id="ppCline"><option value="true">Proxy</option><option value="false">Direct connection</option></select>
        <div class="hint">Cline accounts without proxy bindings follow this switch.</div>
      </div>
      <div class="pp-card">
        <label>TraeWork</label>
        <select id="ppTrae"><option value="true">Proxy</option><option value="false">Direct connection</option></select>
        <div class="hint">TraeWork accounts without proxy bindings follow this switch.</div>
      </div>
      <div class="pp-card">
        <label>WorkBuddy</label>
        <select id="ppWb"><option value="true">Proxy</option><option value="false">Direct connection</option></select>
        <div class="hint">WorkBuddy accounts without proxy bindings follow this switch.</div>
      </div>
      <div class="pp-card">
        <label>OpenCode</label>
        <select id="ppZen"><option value="true">Proxy</option><option value="false">Direct connection</option></select>
        <div class="hint">OpenCode keys without proxy bindings follow this switch.</div>
      </div>
      <div class="pp-card">
        <label>AMDCloud</label>
        <select id="ppAmd"><option value="true">Proxy</option><option value="false">Direct connection</option></select>
        <div class="hint">AMDCloud keys without proxy bindings follow this switch.</div>
      </div>
      <div class="pp-card">
        <label>OpenRouter</label>
        <select id="ppOr"><option value="true">Proxy</option><option value="false">Direct connection</option></select>
        <div class="hint">OpenRouter keys without proxy bindings follow this switch.</div>
      </div>
      <div class="pp-card">
        <label>TokenHarbor</label>
        <select id="ppTh"><option value="true">Proxy</option><option value="false">Direct connection</option></select>
        <div class="hint">TokenHarbor keys without proxy bindings follow this switch.</div>
      </div>
      <div class="pp-card">
        <label>Other</label>
        <select id="ppOther"><option value="true">Proxy</option><option value="false">Direct connection</option></select>
        <div class="hint">Other sub-platform keys without proxy bindings follow this switch.</div>
      </div>
    </div>
    <div class="form-actions"><button class="btn btn-primary" onclick="saveGlobalPolicy()">Save</button></div>
  </div>
</div>
<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><rect x="3" y="11" width="18" height="10" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg></span> Proxy isolation (identity ↔ exit binding)</div>
  <div class="section-body">
    <div class="form-row">
      <div class="field"><label>Isolation mode</label>
        <div style="display:flex;gap:6px;align-items:center">
          <select id="ppIsolation" style="flex:1">
            <option value="true">Enabled (default) — bound identities only ever use their bound exits</option>
            <option value="false">Disabled — ignore bindings; exits rotate per the strategy above (direct when pool off)</option>
          </select>
          <label style="display:flex;gap:6px;align-items:center;white-space:nowrap;margin:0">
            <input type="checkbox" id="ppBackupEnabled" style="width:auto;padding:0;accent-color:var(--accent)">
            Enable backup proxies
          </label>
        </div>
      </div>
    </div>
    <div class="hint">Proxy isolation: each credential only egresses via its bound exits; when none is available it is skipped — never another exit or direct. Forced on when <code>PROXY_ISOLATION</code>=true.</div>
    <div class="hint" id="ppIsolationHint" style="margin-top:4px"></div>
    <div class="hint" id="ppBackupHint" style="margin-top:4px">Backup proxy — off by default: every identity uses its main proxy only; a cooling or removed main proxy is skipped for that round. On: the backup proxy serves as the fallback for the main one.</div>
    <div class="form-actions"><button class="btn btn-primary" onclick="saveIsolationOptions()">Save</button></div>
  </div>
</div>
<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M3 17l5-6 4 3 4-7 5 4"/><path d="M3 21h18"/></svg></span> 代理健康<span style="flex:1"></span><button class="btn btn-sm" onclick="clearProxyHealthStore()" title="Wipe this browser's stored online-rate rows (manual edits and proxies no longer in the pool); the table falls back to current backend data.">Clear data</button></div>
  <div class="section-body">
    <div class="table-wrap">
    <table>
      <thead><tr>
        <th>Proxy</th>
        <th style="text-align:center" title="Upstream attempts that egressed via this proxy since the pool was last edited">Attempts</th>
        <th style="text-align:center" title="Transport-layer success rate since the pool was last edited (upstream 4xx/5xx still counts as the proxy being online)">在线率</th>
        <th style="text-align:center" title="Success rate over the last 50 attempts; red below 80% (min 10 samples).">最近在线率</th>
        <th style="text-align:center;cursor:pointer;user-select:none" onclick="ppHsSortToggle()" title="Average tunnel-setup time over successful attempts (TCP+TLS+CONNECT handshake only — excludes upstream first byte and task duration). Lower is better. Click to sort.">平均握手延迟<span id="ppHsSortMark"></span></th>
        <th>Last OK</th>
        <th>Last Fail</th>
        <th>Cooldown</th>
      </tr></thead>
      <tbody id="ppHealthBody"><tr><td colspan="8" class="empty">Loading...</td></tr></tbody>
    </table>
    </div>
    <div class="hint" style="margin-top:8px">Rates are sampled passively from real traffic — no probing. "No data" = unused since the last pool edit; editing the proxy list resets counters.</div>
    <div class="hint" style="margin-top:4px">Rows follow the backend pool; values persist in this browser to survive pool edits (add-only — a returning proxy picks up its stored numbers). Console override: proxyRateSet('香港-01', 0.42).</div>
  </div>
</div>
</div>

<div id="tab-settings" class="tab-panel" style="display:none">
<h2>Gateway settings</h2>

<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M21 2l-2 2m-7.6 7.6a5.5 5.5 0 1 1-7.78 7.78 5.5 5.5 0 0 1 7.78-7.78zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4"/></svg></span> API keys</div>
  <div class="section-body">
    <p class="hint">Generated keys authenticate client access to the proxy API; they only take effect when the API_KEY environment variable is not set.</p>
    <div class="form-actions" style="margin-bottom:14px">
      <button class="btn btn-success" onclick="generateKey()">Generate new key</button>
    </div>
    <div id="keysList"></div>
    <div id="keyGenResult" style="margin-top:8px"></div>
  </div>
</div>

<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="9"/><path d="M5.6 5.6l12.8 12.8"/></svg></span> Banned platforms <span id="platformBanCount" class="probe-pill" style="font-weight:normal"></span></div>
  <div class="section-body">
    <div class="hint" style="margin-top:0">Banned platforms stop participating in session routing — models leave /v1/models and requests fail immediately; maintenance tasks (check-in etc.) are unaffected. Other sub-platforms can be banned individually. Changes are staged — click Save to save.</div>
    <div style="display:flex;gap:10px;align-items:flex-start;margin-top:10px">
      <div style="flex:1;position:relative">
        <div class="pr-tagbox pb-box" id="platformBanBox" style="margin-top:0;min-height:38px" onclick="togglePlatformBanMenu(event)">
          <span id="platformBanChips" style="display:contents"></span>
          <input class="pr-tagin" readonly placeholder="Click to pick platforms to ban…">
        </div>
        <div id="platformBanMenu" class="pb-menu" style="display:none"></div>
      </div>
      <div style="display:flex;gap:8px;margin-top:4px">
        <button class="btn btn-sm btn-warn btn-static" id="platformBanResetBtn" style="white-space:nowrap" onclick="resetPlatformBans()" title="Discard staged selections and reload the saved ban state">Discard</button>
        <button class="btn btn-sm btn-primary btn-static" id="platformBanApplyBtn" style="white-space:nowrap" onclick="applyPlatformBans()">Save</button>
      </div>
    </div>
  </div>
</div>

<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg></span> Banned models <span id="blacklistCount" class="probe-pill" style="font-weight:normal"></span></div>
  <div class="section-body">
    <div class="hint" style="margin-top:0">Banned automatically (upstream refuses them for this account type, e.g. code 11158) or manually — hidden from /v1/models. Test sends a real request with the first enabled credential; Unblock only removes the entry, the same upstream error bans it again.</div>
    <div class="table-wrap">
      <table>
        <thead><tr><th style="width:140px">Platform</th><th>Model</th><th style="width:190px">Ban reason</th><th style="width:180px">Blocked at</th><th style="width:200px;text-align:center">Actions</th></tr></thead>
        <tbody id="blacklistBody"><tr><td colspan="5" class="empty">Loading...</td></tr></tbody>
      </table>
    </div>
  </div>
</div>

<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M12 3v12m0 0 4-4m-4 4-4-4"/><path d="M4 17v2a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-2"/></svg></span> Config export / import</div>
  <div class="section-body">
    <div class="hint">Export contains credentials — keep the file safe. Import overwrites existing config.</div>
    <div class="form-actions">
      <button class="btn btn-success" onclick="exportConfig()">Export config</button>
      <button class="btn btn-primary" onclick="document.getElementById('importFile').click()">Import config</button>
      <input type="file" id="importFile" accept=".json,application/json" style="display:none" onchange="onImportFileChange(event)">
    </div>
  </div>
</div>

<div class="section danger-zone">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M3 6h18M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2m3 0v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"/></svg></span> Danger zone</div>
  <div class="section-body">
    <div class="hint" style="margin-top:0">Deletes ALL persisted data (accounts, keys, proxies, config). Cannot be undone.</div>
    <div class="form-actions">
      <button class="btn btn-danger" onclick="deleteAllData()">Delete all data</button>
    </div>
  </div>
</div>
</div>

<div id="tab-logs" class="tab-panel" style="display:none">
<div class="flex justify-between" style="margin-bottom:16px">
  <h2>Request logs <span class="probe-pill" style="font-weight:normal">last 500 entries, persisted to data/requests.jsonl</span></h2>
  <div style="display:flex;gap:8px;align-items:center">
    <label style="display:flex;gap:6px;align-items:center;white-space:nowrap;margin:0">
      <input type="checkbox" id="settingAdminLog" onchange="saveAdminLogToggle()" style="width:auto;padding:0;accent-color:var(--accent)">
      Record admin logs
    </label>
    <span class="auto-pill" id="logsAutoPill">Auto-refreshing</span>
    <button class="btn btn-sm" onclick="toggleLogsAuto()" id="logsAutoBtn">Pause</button>
    <button class="btn btn-sm" onclick="loadLogs()">Refresh</button>
  </div>
</div>
<div class="section">
  <div class="section-body" style="padding:6px">
    <div class="table-wrap">
    <table>
      <thead>
        <tr><th>Time</th><th>Source</th><th>Method</th><th>Path</th><th>Model</th><th>Route</th><th>Upstream</th><th>Proxy</th><th>Status</th><th>TTFB</th><th>Duration</th></tr>
      </thead>
      <tbody id="logsTableBody">
        <tr><td colspan="11" class="empty">Loading...</td></tr>
      </tbody>
    </table>
    </div>
  </div>
</div>
</div>

<div id="tab-combos" class="tab-panel" style="display:none">
<h2>Custom aliases</h2>
<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M12 5v14M5 12h14"/></svg></span> Create custom alias</div>
  <div class="section-body">
    <div class="hint" style="margin-bottom:10px">Clients use the alias ID as the model name; the proxy rewrites it to the target. Alias must not collide with real model IDs, and the target must stay on the same platform.</div>
    <div class="form-row">
      <div class="field"><label>Alias ID</label><input type="text" id="comboId" placeholder="cline-glm-5.3"></div>
      <div class="field"><label>Platform</label>
        <select id="comboPlatform" onchange="fillComboModels()"><option value="cline">Cline</option><option value="trae">TraeWork</option><option value="workbuddy">WorkBuddy</option><option value="zen">OpenCode</option><option value="amd">AMDCloud</option><option value="openrouter">OpenRouter</option><option value="tokenharbor">TokenHarbor</option></select>
      </div>
      <div class="field" style="flex:2"><label>Target model</label><select id="comboTarget"></select></div>
      <div class="field" style="flex:0 0 auto;display:flex;align-items:flex-end"><label style="display:flex;gap:6px;align-items:center;white-space:nowrap;padding-bottom:8px"><input type="checkbox" id="comboUseProxies"> Use proxy pool</label></div>
    </div>
    <button class="btn btn-primary" onclick="createCombo()">Create custom alias</button>
  </div>
</div>
<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M8 6h13M8 12h13M8 18h13"/><path d="M3 6h.01M3 12h.01M3 18h.01"/></svg></span> Existing custom aliases</div>
  <div class="section-body" id="combosList">Loading...</div>
</div>
</div>

<div id="tab-opencode" class="tab-panel" style="display:none">
<h2 style="margin-bottom:4px">OpenCode</h2>
<div class="hint" id="ocSummary" style="margin:0 0 16px">Loading...</div>

<div class="section">
  <div class="section-body">
    <div class="form-row">
      <div class="field" style="margin:0">
        <label>Base URL</label>
        <div style="display:flex;gap:8px;align-items:center">
          <input type="text" id="ocBaseURL" placeholder="https://opencode.ai/inference/v1" style="flex:1;min-width:0">
          <button class="btn btn-primary btn-sm" onclick="saveOcConfig()">Confirm</button>
        </div>
      </div>
    </div>
    <div class="flex" style="gap:10px;margin:0 0 8px;align-items:center;flex-wrap:wrap">
      <label class="hint" style="margin:0">Probe model (used by the Test buttons):</label>
      <select id="ocProbeModel" style="max-width:300px"><option value="">auto</option></select>
    </div>
    <div class="table-wrap" style="margin-bottom:10px">
      <table>
        <thead><tr><th style="width:50px">#</th><th style="width:110px">Key</th><th>Status</th><th title="Counted locally by this proxy — not the official free quota">Today/Total tokens</th><th>Last used</th><th title="Main exit first, backup as fallback; both down = row skipped (isolation).">Proxy binding</th><th style="width:130px;text-align:center">Actions</th><th style="text-align:center" title="Routing participation: unchecked keys never enter request rotation. Newly added keys start disabled — tick Pool to enable. Minting follows routing — keys with routing disabled stop auto-minting.">Pool</th></tr></thead>
        <tbody id="ocKeysBody"><tr><td colspan="8" class="empty">Loading...</td></tr></tbody>
      </table>
    </div>
    <div style="display:flex;justify-content:space-between;align-items:center;gap:8px;margin:10px 6px 4px">
      <button class="btn btn-primary btn-sm" onclick="openZenKeyAddDialog()">Add credentials</button>
      <div style="display:flex;gap:8px">
        <button class="btn btn-sm" onclick="stageAllKeysRouting(true)">Enable all</button>
        <button class="btn btn-sm" onclick="stageAllKeysRouting(false)">Disable all</button>
        <button class="btn btn-sm" onclick="stageProxiesByOrder('key')" title="Bind main proxy by row order: 1st credential to Direct, then the proxy pool in order (staged - click Save to apply; enable/disable and backup untouched)">Bind proxies in order</button>
        <button class="btn btn-sm" onclick="stageClearZenKeyProxies()" title="Stage only — click Save to apply. Clears bindings and disables every row.">Clear bindings</button>
        <button class="btn btn-sm" id="ocTestBtn" onclick="batchTestKeys()" title="Run the Test probe on every row.">Test all</button>
      </div>
      <div style="display:flex;gap:8px">
        <button class="btn btn-sm btn-warn" id="ocResetBtn" onclick="resetKeyEdits()" title="Discard staged changes and reload the last saved state">Discard</button>
        <button class="btn btn-primary btn-sm" id="ocKeysSaveBtn" onclick="saveKeyEdits()">Save</button>
      </div>
    </div>
  </div>
</div>

<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg></span> Rate-limit defense</div>
  <div class="section-body">
    <div class="form-row">
      <div class="field"><label>Max concurrency</label><input type="text" id="ocMaxConc" placeholder="8"></div>
      <div class="field"><label>Rate-limit retries</label><input type="text" id="ocRetries" placeholder="3"></div>
    </div>
    <div class="form-actions"><button class="btn btn-primary" onclick="saveOcConfig()">Save rate-limit config</button></div>
  </div>
</div>

<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M4 8V6a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v2M4 8v10a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8M4 8h16"/><path d="M10 12h4"/></svg></span> Context compaction (OpenCode official mechanism)</div>
  <div class="section-body">
    <div class="form-row">
      <div class="field"><label>Auto-compaction</label>
        <select id="ocCompactAuto"><option value="true">On</option><option value="false">Off</option></select>
      </div>
      <div class="field"><label>Reserved buffer</label><input type="text" id="ocCompactBuffer" placeholder="20000"></div>
    </div>
    <div class="form-row">
      <div class="field"><label>Tail tokens kept</label><input type="text" id="ocKeepTokens" placeholder="8000"></div>
      <div class="field"><label>Summary model</label><input type="text" id="ocSummaryModel" placeholder="empty = same as request model"></div>
    </div>
    <div class="form-row">
      <div class="field"><label>Summary cap</label><input type="text" id="ocMaxSummary" placeholder="4096"></div>
    </div>
    <div class="form-actions"><button class="btn btn-primary" onclick="saveOcConfig()">Save compaction config</button></div>
  </div>
</div>

<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg></span> Live session IDs (OpenCode FreeTier gate)
    <span id="ocSessSummary" class="probe-pill" style="font-weight:normal;margin-left:auto"></span>
  </div>
  <div class="section-body">
    <p class="hint" style="margin-top:0" id="ocSessHint">The gateway mints a CLI-format session ID per key on first use and keeps it sticky; IDs persist in the data volume and are refreshed locally after repeated 403s.</p>
    <div class="table-wrap">
      <table>
        <thead><tr><th style="width:70px">Key</th><th style="width:130px">Session</th><th style="width:150px">State</th><th>Minted at</th></tr></thead>
        <tbody id="ocSessBody"><tr><td colspan="4" class="empty">Loading...</td></tr></tbody>
      </table>
    </div>
  </div>
</div>

<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M3 3v18h18"/><path d="M7 15l4-4 3 3 5-6"/></svg></span> Model catalog <span class="probe-pill" data-models-info style="font-weight:normal"></span></div>
  <div class="section-body">
    <div class="flex" style="margin-bottom:10px;gap:10px">
      <button class="btn btn-sm btn-primary" onclick="refreshModels()">Refresh models</button>
      <span class="hint" style="margin:0">Automatically synced from the official model source upstream (every 5 minutes)</span>
    </div>
    <div id="ocModelsBody">Loading...</div>
  </div>
</div>
</div>

<div id="tab-workbuddy" class="tab-panel" style="display:none">
  <div class="flex justify-between" style="margin-bottom:16px">
  <div>
    <h2 style="margin-bottom:4px">WorkBuddy</h2>
    <div class="hint" id="wbSummary" style="margin:0">Loading...</div>
  </div>
  <a class="btn btn-sm" href="/admin/workbuddy/" target="_blank" rel="noopener" style="align-self:flex-start">Original panel</a>
</div>

<div class="section">
  <div class="section-body" style="padding:6px">
    <div style="display:flex;justify-content:space-between;align-items:center;gap:8px;margin:4px 6px 10px">
      <span class="wb-toolbar">
        <button class="btn btn-sm" onclick="openWorkbuddyTodoScan()">Task center</button>
        <button class="btn btn-sm" onclick="refreshWorkbuddyAccounts()" title="Refresh all account balances">Refresh all</button>
      </span>
      <span class="wb-toolbar">
        <button class="btn btn-sm" onclick="runWorkbuddyBatch('checkin_all')">Check in all</button>
        <button class="btn btn-sm" onclick="runWorkbuddyBatch('travel_all')">Pet patrol</button>
        <button class="btn btn-sm" onclick="runWorkbuddyBatch('activity_all')">Activity all</button>
        <button class="btn btn-sm" onclick="runWorkbuddyBatch('keepalive_all')">Refresh login</button>
      </span>
    </div>
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th style="width:44px">#</th>
            <th>Account</th>
            <th>Status</th>
            <th>Credits</th>
            <th title="Counted locally by this proxy — not the official free quota">Today/Total tokens</th>
            <th>Last used</th>
            <th title="Main exit first, backup as fallback; both down = row skipped (isolation).">Proxy binding</th>
            <th style="text-align:center">Actions</th>
            <th style="text-align:center" title="Gateway (reverse proxy) switch: unchecked = model sessions never route to this account; check-in and all other maintenance tasks are unaffected. Can only be ticked while the account switch is on.">Gateway switch</th>
            <th style="text-align:center" title="Account master switch. Off = the account does not exist: check-in, patrol, activity, keepalive, night chats, growth and balance refresh all skip it, and it never routes.">Account switch</th>
          </tr>
        </thead>
        <tbody id="wbAccountsBody">
          <tr><td colspan="10" class="empty">Loading...</td></tr>
        </tbody>
      </table>
    </div>
    <div style="display:flex;align-items:center;gap:8px;margin:10px 6px 4px">
      <div style="flex:1;display:flex;gap:8px;justify-content:flex-start">
        <button class="btn btn-primary btn-sm" onclick="openWorkbuddyAdd()">Add account</button>
        <button class="btn btn-sm" id="wbTestAllBtn" onclick="batchTestWorkbuddyAccounts()" title="Run the Test probe on every row.">Test all</button>
      </div>
      <div style="flex:1;display:flex;gap:8px;justify-content:center">
        <button class="btn btn-sm" onclick="stageAllWbPool(true)">Enable all</button>
        <button class="btn btn-sm" onclick="stageAllWbPool(false)">Disable all</button>
        <button class="btn btn-sm" onclick="stageProxiesByOrder('wb')" title="Bind main proxy by row order: 1st credential to Direct, then the proxy pool in order (staged - click Save to apply; enable/disable and backup untouched)">Bind proxies in order</button>
        <button class="btn btn-sm" onclick="stageClearWorkbuddyProxyBindings()" title="Stage only — click Save to apply. Clears bindings and disables every row.">Clear bindings</button>
      </div>
      <div style="flex:1;display:flex;gap:8px;justify-content:flex-end">
        <button class="btn btn-sm btn-warn" id="wbResetBtn" onclick="resetWorkbuddyEdits()" title="Discard staged changes and reload the last saved state">Discard</button>
        <button class="btn btn-primary btn-sm" id="wbSaveBtn" onclick="saveWorkbuddyEdits()">Save</button>
      </div>
    </div>
  </div>
</div>

<form id="wbConfigForm">
  <div class="section">
    <div class="section-title">
      <span class="sec-ico"><svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .34 1.87l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.7 1.7 0 0 0-1.87.34 1.7 1.7 0 0 0-1 1.55V21a2 2 0 1 1-4 0v-.09a1.7 1.7 0 0 0-1-1.55 1.7 1.7 0 0 0-1.87.34l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.7 1.7 0 0 0 .34-1.87 1.7 1.7 0 0 0-1.55-1H3a2 2 0 1 1 0-4h.09a1.7 1.7 0 0 0 1.55-1 1.7 1.7 0 0 0-.34-1.87l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.7 1.7 0 0 0 1.87.34h0a1.7 1.7 0 0 0 1-1.55V3a2 2 0 1 1 4 0v.09a1.7 1.7 0 0 0 1 1.55h0a1.7 1.7 0 0 0 1.87-.34l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.7 1.7 0 0 0-.34 1.87v0a1.7 1.7 0 0 0 1.55 1H21a2 2 0 1 1 0 4h-.09a1.7 1.7 0 0 0-1.55 1z"/></svg></span>
      WorkBuddy configuration
      <span style="flex:1"></span>
      <span class="auto-pill" id="wbConfigNote">Managed by the admin session</span>
    </div>
    <div class="section-body">
      <div class="wb-config-grid">
        <div class="wb-config-group">
          <h3>Scheduled jobs</h3>
          <div class="wb-switch"><input type="checkbox" id="wbCheckinEnabled" data-wb-path="schedule.checkin_enabled"><label for="wbCheckinEnabled">Automatic check-in</label></div>
          <div class="wb-field"><label for="wbCheckinHours">Check-in hours</label><input id="wbCheckinHours" data-wb-path="schedule.checkin_hours" data-wb-type="ints" placeholder="9, 21"></div>
          <div class="wb-switch"><input type="checkbox" id="wbTravelEnabled" data-wb-path="schedule.travel_enabled"><label for="wbTravelEnabled">Automatic patrol</label></div>
          <div class="wb-field"><label for="wbTravelHours">Patrol hours</label><input id="wbTravelHours" data-wb-path="schedule.travel_hours" data-wb-type="ints" placeholder="9, 21"></div>
          <div class="wb-switch"><input type="checkbox" id="wbActivityEnabled" data-wb-path="schedule.activity_enabled"><label for="wbActivityEnabled">Automatic activity</label></div>
          <div class="wb-field"><label for="wbActivityHours">Activity hours</label><input id="wbActivityHours" data-wb-path="schedule.activity_hours" data-wb-type="ints" placeholder="10"></div>
          <div class="wb-switch"><input type="checkbox" id="wbKeepaliveEnabled" data-wb-path="schedule.keepalive_enabled"><label for="wbKeepaliveEnabled">Automatic keepalive</label></div>
          <div class="wb-field"><label for="wbKeepaliveHours">Keepalive hours</label><input id="wbKeepaliveHours" data-wb-path="schedule.keepalive_hours" data-wb-type="ints" placeholder="22"></div>
          <div class="wb-switch"><input type="checkbox" id="wbBalanceEnabled" data-wb-path="schedule.balance_refresh_enabled"><label for="wbBalanceEnabled">Background balance refresh</label></div>
          <div class="wb-field"><label for="wbBalanceMinutes">Refresh interval (min)</label><input id="wbBalanceMinutes" data-wb-path="schedule.balance_refresh_minutes" data-wb-type="number" min="1" placeholder="5"></div>
          <div class="wb-switch"><input type="checkbox" id="wbGrowthEnabled" data-wb-path="schedule.growth_enabled"><label for="wbGrowthEnabled">Growth tasks</label></div>
          <div class="wb-field"><label for="wbGrowthHours">Growth hours</label><input id="wbGrowthHours" data-wb-path="schedule.growth_hours" data-wb-type="ints" placeholder="1"></div>
        </div>

        <div class="wb-config-group">
          <h3>Pool and cooldown</h3>
          <div class="wb-field"><label for="wbMaxInFlight">Max in-flight / account</label><input id="wbMaxInFlight" data-wb-path="pool.max_in_flight" data-wb-type="number" min="0" placeholder="3"></div>
          <div class="wb-field"><label for="wbModelRateFilter">Model rate filter</label>
            <select id="wbModelRateFilter" data-wb-path="pool.model_rate_filter" data-wb-type="number">
              <option value="0">0 — free only</option>
              <option value="0.1">0.1</option>
              <option value="0.2">0.2</option>
              <option value="0.3">0.3</option>
              <option value="0.5">0.5</option>
              <option value="1">1</option>
            </select>
          </div>
          <div class="wb-field"><label for="wbMaxGlobal">Global in-flight limit</label><input id="wbMaxGlobal" data-wb-path="pool.max_in_flight_global" data-wb-type="number" min="1" placeholder="2"></div>
          <div class="wb-field"><label for="wbBreakerThreshold">Breaker threshold</label><input id="wbBreakerThreshold" data-wb-path="pool.breaker_threshold" data-wb-type="number" min="1" placeholder="3"></div>
          <div class="wb-field"><label for="wbSoftRate">Soft rate cooldown</label><input id="wbSoftRate" data-wb-path="cooldown.soft_rate" data-wb-type="duration" placeholder="600s"></div>
          <div class="wb-field"><label for="wbSoftMax">Soft cooldown max</label><input id="wbSoftMax" data-wb-path="cooldown.soft_rate_max" data-wb-type="duration" placeholder="2h"></div>
          <div class="wb-field"><label for="wbBreakerCooldown">Breaker cooldown</label><input id="wbBreakerCooldown" data-wb-path="pool.breaker_cooldown" data-wb-type="duration" placeholder="30m"></div>
          <div class="wb-field"><label for="wbBreakerMax">Breaker cooldown max</label><input id="wbBreakerMax" data-wb-path="pool.breaker_cooldown_max" data-wb-type="duration" placeholder="6h"></div>
          <div class="wb-field"><label for="wbDegradeThreshold">Degrade threshold</label><input id="wbDegradeThreshold" data-wb-path="pool.degrade_threshold" data-wb-type="number" min="1" placeholder="5"></div>
          <div class="wb-field"><label for="wbDegradeCooldown">Degrade cooldown</label><input id="wbDegradeCooldown" data-wb-path="pool.degrade_cooldown" data-wb-type="duration" placeholder="10m"></div>
          <div class="wb-field"><label for="wbDegradeMax">Degrade cooldown max</label><input id="wbDegradeMax" data-wb-path="pool.degrade_cooldown_max" data-wb-type="duration" placeholder="2h"></div>
          <div class="wb-field"><label for="wbIdleWeight">Idle weight / hour</label><input id="wbIdleWeight" data-wb-path="pool.idle_weight_per_hour" data-wb-type="number" step="0.1" placeholder="0.5"></div>
          <div class="wb-field"><label for="wbIdleMax">Idle weight max</label><input id="wbIdleMax" data-wb-path="pool.idle_weight_max" data-wb-type="number" step="0.1" placeholder="5"></div>
          <div class="wb-field"><label for="wbCostExplore">Cost explore window</label><input id="wbCostExplore" data-wb-path="pool.cost_explore_interval" data-wb-type="duration" placeholder="30m"></div>
          <div class="wb-field"><label for="wbStickyTTL">Sticky TTL</label><input id="wbStickyTTL" data-wb-path="session_sticky.ttl" data-wb-type="duration" placeholder="30m"></div>
        </div>

        <div class="wb-config-group">
          <h3>Session and upstream</h3>
          <div class="wb-field"><label for="wbTimeout">Short request timeout</label><input id="wbTimeout" data-wb-path="upstream.timeout_seconds" data-wb-type="number" min="1" placeholder="120"></div>
          <div class="wb-field"><label for="wbHeaderTimeout">First-byte timeout</label><input id="wbHeaderTimeout" data-wb-path="upstream.header_timeout_seconds" data-wb-type="number" min="1" placeholder="120"></div>
          <div class="wb-field"><label for="wbIdleTimeout">Stream idle timeout</label><input id="wbIdleTimeout" data-wb-path="upstream.idle_timeout_seconds" data-wb-type="number" min="1" placeholder="300"></div>
          <div class="wb-field"><label for="wbUserAgent">User-Agent</label><input id="wbUserAgent" data-wb-path="upstream.user_agent" data-wb-send-empty="1" placeholder="Leave empty = CLI / 2.171.0 CodeBuddy / 2.171.0"></div>
          <div class="wb-field">
            <label for="wbPromptMode">System prompt mode</label>
            <select id="wbPromptMode" data-wb-path="prompt.mode">
              <option value="custom">custom — gateway-owned prompt (avoids fingerprint false positives)</option>
              <option value="append">append — insert gateway prompt after client system (both apply)</option>
              <option value="passthrough">passthrough — pass client system through unchanged</option>
            </select>
          </div>
          <div class="wb-field"><label for="wbPromptFile">Prompt file</label><input id="wbPromptFile" data-wb-path="prompt.file" data-wb-send-empty="1" placeholder="Leave empty = built-in default prompt"></div>
          <div class="wb-switch"><input type="checkbox" id="wbSanitize" data-wb-path="features.sanitize_blacklist_fingerprints"><label for="wbSanitize">Sanitize outbound fingerprints</label></div>
          <div class="wb-switch"><input type="checkbox" id="wbStickyEnabled" data-wb-path="session_sticky.enabled"><label for="wbStickyEnabled">Sticky session routing</label></div>
          <div class="hint" style="margin-top:10px">Upstash Redis mirror, auth dir, and state file are edited in the config file manually (restart items).</div>
        </div>
      </div>
      <div class="form-actions" style="justify-content:flex-end">
        <button type="button" class="btn" onclick="loadWorkbuddyConfig()">Discard changes</button>
        <button type="submit" class="btn btn-primary" id="wbSaveConfig">Save configuration</button>
      </div>
    </div>
  </div>
</form>

<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M3 3v18h18"/><path d="M7 15l4-4 3 3 5-6"/></svg></span> Model catalog <span class="probe-pill" data-models-info style="font-weight:normal"></span></div>
  <div class="section-body">
    <div class="flex" style="margin-bottom:10px;gap:10px">
      <button class="btn btn-sm btn-primary" onclick="refreshWbModels()">Refresh models</button>
      <span class="hint" style="margin:0">Automatically synced from the official model source upstream (every 5 minutes)</span>
    </div>
    <div id="wbModelsBody">Loading...</div>
  </div>
</div>
</div>

<div id="tab-openrouter" class="tab-panel" style="display:none">
<h2 style="margin-bottom:4px">OpenRouter</h2>
<div class="hint" id="orSummary" style="margin:0 0 16px">Loading...</div>



<div class="section">
  <div class="section-body">
    <div class="form-row">
      <div class="field" style="margin:0">
        <label>Base URL</label>
        <div style="display:flex;gap:8px;align-items:center">
          <input type="text" id="orBaseURL" placeholder="https://openrouter.ai/api/v1" style="flex:1;min-width:0">
          <button class="btn btn-primary btn-sm" onclick="saveOrConfig()">Confirm</button>
        </div>
      </div>
    </div>
    <div class="flex" style="gap:10px;margin:8px 0;align-items:center;flex-wrap:wrap">
      <label class="hint" style="margin:0">Probe model (used by the Test buttons):</label>
      <select id="orProbeModel" style="max-width:420px"><option value="">auto</option></select>
      <button class="btn btn-sm" onclick="refreshOrModels()" title="Re-sync the free-model catalog from openrouter.ai">Refresh models</button>
      <span class="hint" style="margin:0" id="orModelsCount"></span>
    </div>
    <div class="table-wrap" style="margin-bottom:10px">
      <table>
        <thead><tr><th style="width:50px">#</th><th style="width:150px">Key</th><th>Status</th><th title="Counted locally by this proxy — not the official free quota">Today/Total tokens</th><th>Last used</th><th title="Main exit first, backup as fallback; both down = row skipped (isolation).">Proxy binding</th><th style="width:130px;text-align:center">Actions</th><th style="text-align:center" title="Routing participation. New rows start disabled — tick Pool to enable.">Pool</th></tr></thead>
        <tbody id="orKeysBody"><tr><td colspan="8" class="empty">Loading...</td></tr></tbody>
      </table>
    </div>
    <div style="display:flex;justify-content:space-between;align-items:center;gap:8px;margin:10px 6px 4px">
      <button class="btn btn-primary btn-sm" onclick="openOrKeyAddDialog()">Add credentials</button>
      <div style="display:flex;gap:8px">
        <button class="btn btn-sm" onclick="stageAllOrKeysRouting(true)">Enable all</button>
        <button class="btn btn-sm" onclick="stageAllOrKeysRouting(false)">Disable all</button>
        <button class="btn btn-sm" onclick="stageProxiesByOrder('or')" title="Bind main proxy by row order: 1st credential to Direct, then the proxy pool in order (staged - click Save to apply; enable/disable and backup untouched)">Bind proxies in order</button>
        <button class="btn btn-sm" onclick="stageClearOrKeyProxies()" title="Stage only — click Save to apply. Clears bindings and disables every row.">Clear bindings</button>
        <button class="btn btn-sm" id="orTestBtn" onclick="batchTestOrKeys()" title="Run the Test probe on every row.">Test all</button>
      </div>
      <div style="display:flex;gap:8px">
        <button class="btn btn-sm btn-warn" id="orResetBtn" onclick="resetOrKeyEdits()" title="Discard staged changes and reload the last saved state">Discard</button>
        <button class="btn btn-primary btn-sm" id="orKeysSaveBtn" onclick="saveOrKeyEdits()">Save</button>
      </div>
    </div>
  </div>
</div>

<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg></span> Rate-limit defense</div>
  <div class="section-body">
    <div class="form-row">
      <div class="field"><label>Max concurrency</label><input type="text" id="orMaxConc" placeholder="8"></div>
      <div class="field"><label>Rate-limit retries</label><input type="text" id="orRetries" placeholder="3"></div>
    </div>
    <div class="form-actions"><button class="btn btn-primary" onclick="saveOrConfig()">Save rate-limit config</button></div>
  </div>
</div>

<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M3 3v18h18"/><path d="M7 15l4-4 3 3 5-6"/></svg></span> Model catalog</div>
  <div class="section-body">
        <div class="flex" style="margin-bottom:10px;gap:10px">
      <button class="btn btn-sm btn-primary" onclick="refreshOrModels()">Refresh models</button>
      <span class="hint" style="margin:0">Automatically synced from the official model source upstream (every 5 minutes)</span>
    </div>
    <div id="orModelsBody">Loading...</div>
    </div>
  </div>
</div>

<div id="tab-amdcloud" class="tab-panel" style="display:none">
<h2 style="margin-bottom:4px">AMDCloud</h2>
<div class="hint" id="amdSummary" style="margin:0 0 16px">Loading...</div>



<div class="section">
  <div class="section-body">
    <div class="form-row">
      <div class="field" style="margin:0">
        <label>Base URL</label>
        <div style="display:flex;gap:8px;align-items:center">
          <input type="text" id="amdBaseURL" placeholder="https://developer.amd.com.cn/radeon/api/v1" style="flex:1;min-width:0">
          <button class="btn btn-primary btn-sm" onclick="saveAmdConfig()">Confirm</button>
        </div>
      </div>
    </div>
    <div class="flex" style="gap:10px;margin:8px 0;align-items:center;flex-wrap:wrap">
      <label class="hint" style="margin:0">Probe model (used by the Test buttons):</label>
      <select id="amdProbeModel" style="max-width:420px"><option value="">auto</option></select>
      <button class="btn btn-sm" onclick="refreshAmdModels()" title="Re-sync the model catalog from AMDCloud">Refresh models</button>
      <span class="hint" style="margin:0" id="amdModelsCount"></span>
    </div>
    <div class="table-wrap" style="margin-bottom:10px">
      <table>
        <thead><tr><th style="width:50px">#</th><th style="width:150px">Key</th><th>Status</th><th title="Counted locally by this proxy — not the official free quota">Today/Total tokens</th><th>Last used</th><th title="Main exit first, backup as fallback; both down = row skipped (isolation).">Proxy binding</th><th style="width:130px;text-align:center">Actions</th><th style="text-align:center" title="Routing participation. New rows start disabled — tick Pool to enable.">Pool</th></tr></thead>
        <tbody id="amdKeysBody"><tr><td colspan="8" class="empty">Loading...</td></tr></tbody>
      </table>
    </div>
    <div style="display:flex;justify-content:space-between;align-items:center;gap:8px;margin:10px 6px 4px">
      <button class="btn btn-primary btn-sm" onclick="openAmdKeyAddDialog()">Add credentials</button>
      <div style="display:flex;gap:8px">
        <button class="btn btn-sm" onclick="stageAllAmdKeysRouting(true)">Enable all</button>
        <button class="btn btn-sm" onclick="stageAllAmdKeysRouting(false)">Disable all</button>
        <button class="btn btn-sm" onclick="stageProxiesByOrder('amd')" title="Bind main proxy by row order: 1st credential to Direct, then the proxy pool in order (staged - click Save to apply; enable/disable and backup untouched)">Bind proxies in order</button>
        <button class="btn btn-sm" onclick="stageClearAmdKeyProxies()" title="Stage only — click Save to apply. Clears bindings and disables every row.">Clear bindings</button>
        <button class="btn btn-sm" id="amdTestBtn" onclick="batchTestAmdKeys()" title="Run the Test probe on every row.">Test all</button>
      </div>
      <div style="display:flex;gap:8px">
        <button class="btn btn-sm btn-warn" id="amdResetBtn" onclick="resetAmdKeyEdits()" title="Discard staged changes and reload the last saved state">Discard</button>
        <button class="btn btn-primary btn-sm" id="amdKeysSaveBtn" onclick="saveAmdKeyEdits()">Save</button>
      </div>
    </div>
  </div>
</div>

<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg></span> Rate-limit defense</div>
  <div class="section-body">
    <div class="hint" style="margin-top:0;margin-bottom:14px">Upstream limits: 30 requests/min per key, 8 concurrent per key, 20 requests/min per account.</div>
    <div class="form-row">
      <div class="field"><label>Max concurrency</label><input type="text" id="amdMaxConc" placeholder="8"></div>
      <div class="field"><label>Rate-limit retries</label><input type="text" id="amdRetries" placeholder="3"></div>
    </div>
    <div class="form-actions"><button class="btn btn-primary" onclick="saveAmdConfig()">Save rate-limit config</button></div>
  </div>
</div>

<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M3 3v18h18"/><path d="M7 15l4-4 3 3 5-6"/></svg></span> Model catalog</div>
  <div class="section-body">
        <div class="flex" style="margin-bottom:10px;gap:10px">
      <button class="btn btn-sm btn-primary" onclick="refreshAmdModels()">Refresh models</button>
      <span class="hint" style="margin:0">Automatically synced from the official model source upstream (every 5 minutes)</span>
    </div>
    <div id="amdModelsBody">Loading...</div>
    </div>
  </div>
</div>

<div id="tab-tokenharbor" class="tab-panel" style="display:none">
<h2 style="margin-bottom:4px">TokenHarbor</h2>
<div class="hint" id="thSummary" style="margin:0 0 16px">Loading...</div>



<div class="section">
  <div class="section-body">
    <div class="form-row">
      <div class="field" style="margin:0">
        <label>Base URL</label>
        <div style="display:flex;gap:8px;align-items:center">
          <input type="text" id="thBaseURL" placeholder="https://tokenharbor.ai/v1" style="flex:1;min-width:0">
          <button class="btn btn-primary btn-sm" onclick="saveTHConfig()">Confirm</button>
        </div>
      </div>
    </div>
    <div class="flex" style="gap:10px;margin:8px 0;align-items:center;flex-wrap:wrap">
      <label class="hint" style="margin:0">Probe model (used by the Test buttons):</label>
      <select id="thProbeModel" style="max-width:420px"><option value="">auto</option></select>
      <button class="btn btn-sm" onclick="refreshTHModels()" title="Re-sync the free-model catalog from TokenHarbor (needs a key)">Refresh models</button>
      <span class="hint" style="margin:0" id="thModelsCount"></span>
    </div>
    <div class="table-wrap" style="margin-bottom:10px">
      <table>
        <thead><tr><th style="width:50px">#</th><th style="width:150px">Key</th><th>Status</th><th title="Counted locally by this proxy — not the official free quota">Today/Total tokens</th><th>Last used</th><th title="Main exit first, backup as fallback; both down = row skipped (isolation).">Proxy binding</th><th style="width:130px;text-align:center">Actions</th><th style="text-align:center" title="Routing participation. New rows start disabled — tick Pool to enable.">Pool</th></tr></thead>
        <tbody id="thKeysBody"><tr><td colspan="8" class="empty">Loading...</td></tr></tbody>
      </table>
    </div>
    <div style="display:flex;justify-content:space-between;align-items:center;gap:8px;margin:10px 6px 4px">
      <button class="btn btn-primary btn-sm" onclick="openTHKeyAddDialog()">Add credentials</button>
      <div style="display:flex;gap:8px">
        <button class="btn btn-sm" onclick="stageAllTHKeysRouting(true)">Enable all</button>
        <button class="btn btn-sm" onclick="stageAllTHKeysRouting(false)">Disable all</button>
        <button class="btn btn-sm" onclick="stageProxiesByOrder('th')" title="Bind main proxy by row order: 1st credential to Direct, then the proxy pool in order (staged - click Save to apply; enable/disable and backup untouched)">Bind proxies in order</button>
        <button class="btn btn-sm" onclick="stageClearTHKeyProxies()" title="Stage only — click Save to apply. Clears bindings and disables every row.">Clear bindings</button>
        <button class="btn btn-sm" id="thTestBtn" onclick="batchTestTHKeys()" title="Run the Test probe on every row.">Test all</button>
      </div>
      <div style="display:flex;gap:8px">
        <button class="btn btn-sm btn-warn" id="thResetBtn" onclick="resetTHKeyEdits()" title="Discard staged changes and reload the last saved state">Discard</button>
        <button class="btn btn-primary btn-sm" id="thKeysSaveBtn" onclick="saveTHKeyEdits()">Save</button>
      </div>
    </div>
  </div>
</div>

<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg></span> Rate-limit defense</div>
  <div class="section-body">
    <div class="hint" style="margin-top:0;margin-bottom:14px">Upstream limits: 60 requests/min per account, 100 requests/min per IP.</div>
    <div class="form-row">
      <div class="field"><label>Max concurrency</label><input type="text" id="thMaxConc" placeholder="8"></div>
      <div class="field"><label>Rate-limit retries</label><input type="text" id="thRetries" placeholder="3"></div>
    </div>
    <div class="form-actions"><button class="btn btn-primary" onclick="saveTHConfig()">Save rate-limit config</button></div>
  </div>
</div>

<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M3 3v18h18"/><path d="M7 15l4-4 3 3 5-6"/></svg></span> Model catalog</div>
  <div class="section-body">
        <div class="flex" style="margin-bottom:10px;gap:10px">
      <button class="btn btn-sm btn-primary" onclick="refreshTHModels()">Refresh models</button>
      <span class="hint" style="margin:0">Automatically synced from the official model source upstream (every 5 minutes)</span>
    </div>
    <div id="thModelsBody">Loading...</div>
    </div>
  </div>
</div>

<div id="tab-other" class="tab-panel" style="display:none">
<h2 style="margin-bottom:4px">Other</h2>
<div class="hint" id="otherSummary" style="margin:0 0 16px">Loading...</div>

<div class="section">
  <div class="section-body">
    <div class="form-row">
      <div class="field" style="margin:0">
        <label>Sub-platform</label>
        <div style="display:flex;gap:8px;align-items:center">
          <select id="otherPlatformSelect" style="flex:1;min-width:0"><option value="">— no sub-platform yet —</option></select>
          <button class="btn btn-primary btn-sm" onclick="openOtherPlatformDialog()">New sub-platform</button>
          <button class="btn btn-sm" id="otherPlatformManageBtn" onclick="openOtherManageDialog()" title="Reorder or delete sub-platforms">Manage</button>
        </div>
      </div>
    </div>
  </div>
</div>

<div id="otherPlatformBody" style="display:none">

<div class="section">
  <div class="section-body">
    <div class="form-row">
      <div class="field"><label>Name</label><input type="text" id="otherName" placeholder="Acme Cloud"></div>
      <div class="field"><label>Model prefix (required)</label><input type="text" id="otherPrefix" placeholder="acme" style="font-family:'JetBrains Mono',Consolas,monospace"></div>
    </div>
    <div class="form-row">
      <div class="field" style="margin:0">
        <label>Base URL</label>
        <div style="display:flex;gap:8px;align-items:center">
          <input type="text" id="otherBaseURL" placeholder="https://api.acme.com/v1" style="flex:1;min-width:0">
          <button class="btn btn-primary btn-sm" onclick="saveOtherPlatformConfig()">Confirm</button>
        </div>
      </div>
    </div>
    <div class="form-row" style="margin-top:10px;flex-wrap:nowrap">
      <div class="field" style="flex:1 1 0;min-width:0"><label>Max concurrency</label><input type="text" id="otherMaxConc" placeholder="8"></div>
      <div class="field" style="flex:1 1 0;min-width:0"><label>Rate-limit retries</label><input type="text" id="otherRetries" placeholder="3"></div>
      <div class="field" style="flex:0 0 auto;min-width:0;align-self:stretch;display:grid;grid-template-rows:auto 1fr;align-items:center"><label aria-hidden="true" style="visibility:hidden">&nbsp;</label><button class="btn btn-primary btn-sm" onclick="saveOtherRateLimit()">Save rate-limit config</button></div>
    </div>
  </div>
</div>

<div class="section">
  <div class="section-body">
    <div class="table-wrap" style="margin-bottom:10px">
      <table>
        <thead><tr><th style="width:50px">#</th><th style="width:150px">Key</th><th>Status</th><th title="Counted locally by this proxy — not the official free quota">Today/Total tokens</th><th>Last used</th><th title="Main exit first, backup as fallback; both down = row skipped (isolation).">Proxy binding</th><th style="width:130px;text-align:center">Actions</th><th style="text-align:center" title="Routing participation. New rows start disabled — tick Pool to enable.">Pool</th></tr></thead>
        <tbody id="otherKeysBody"><tr><td colspan="8" class="empty">No keys configured</td></tr></tbody>
      </table>
    </div>
    <div style="display:flex;justify-content:space-between;align-items:center;gap:8px;margin:10px 6px 4px">
      <button class="btn btn-primary btn-sm" onclick="openOtherKeyAddDialog()">Add credentials</button>
      <div style="display:flex;gap:8px">
        <button class="btn btn-sm" onclick="stageAllOtherKeysRouting(true)">Enable all</button>
        <button class="btn btn-sm" onclick="stageAllOtherKeysRouting(false)">Disable all</button>
        <button class="btn btn-sm" onclick="stageProxiesByOrder('other')" title="Bind main proxy by row order: 1st credential to Direct, then the proxy pool in order (staged - click Save to apply; enable/disable and backup untouched)">Bind proxies in order</button>
        <button class="btn btn-sm" onclick="stageClearOtherKeyProxies()" title="Stage only — click Save to apply. Clears bindings and disables every row.">Clear bindings</button>
        <button class="btn btn-sm" id="otherTestBtn" onclick="batchTestOtherKeys()" title="Run the Test probe on every row.">Test all</button>
      </div>
      <div style="display:flex;gap:8px">
        <button class="btn btn-sm btn-warn" id="otherResetBtn" onclick="resetOtherKeyEdits()" title="Discard staged changes and reload the last saved state">Discard</button>
        <button class="btn btn-primary btn-sm" id="otherKeysSaveBtn" onclick="saveOtherKeyEdits()">Save</button>
      </div>
    </div>
  </div>
</div>

<div class="section" style="overflow:visible">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M3 3v18h18"/><path d="M7 15l4-4 3 3 5-6"/></svg></span> Models</div>
  <div class="section-body" style="overflow:visible">
    <!-- 第一行：模型多选下拉（占满剩余宽度） + 刷新按钮 -->
    <div class="flex" style="gap:10px;margin:0 0 10px;align-items:stretch;flex-wrap:wrap;position:relative">
      <div class="multi-select" id="otherModelSelect" style="flex:1 1 320px;min-width:240px">
        <button type="button" class="ms-toggle" id="otherModelSelectToggle" onclick="toggleOtherModelSelect()" style="width:100%">—</button>
        <div class="ms-panel" id="otherModelSelectPanel" style="display:none">
          <div class="ms-search"><input type="text" id="otherModelSelectSearch" placeholder="Search models…" oninput="filterOtherModelSelect()"></div>
          <div class="ms-list" id="otherModelSelectList"></div>
        </div>
      </div>
      <button class="btn btn-sm" onclick="refreshOtherModels()" style="flex:0 0 auto;align-self:center" title="Re-sync the model catalog from the upstream (needs a credential)">Refresh models</button>
    </div>
    <!-- 第二行：左半=手动添加模型，右半=真实会话测试，各占一半 -->
    <div class="flex" style="gap:10px;margin:0;align-items:center">
      <div class="flex" style="gap:8px;align-items:center;flex:1 1 0;min-width:0">
        <input type="text" id="otherManualModel" placeholder="add model id manually" style="flex:1 1 auto;min-width:0;font-family:'JetBrains Mono',Consolas,monospace">
        <button class="btn btn-sm" onclick="addOtherManualModel()" style="flex:0 0 auto">Add model</button>
      </div>
      <div class="flex" style="gap:8px;align-items:center;flex:1 1 0;min-width:0">
        <input type="text" id="otherLivePrompt" style="flex:1 1 auto;min-width:0">
        <button class="btn btn-sm" id="otherLiveBtn" onclick="otherLiveRequest()" style="flex:0 0 auto" title="Send a real chat request using the first enabled credential">Real request</button>
      </div>
    </div>
  </div>
</div>

</div>
</div>

</div>
</div>

<div id="toast" class="toast" popover="manual"></div>

<dialog id="wbAddDialog">
  <div class="wb-dialog-head">
    <span>Add WorkBuddy account</span>
    <button class="btn btn-sm" onclick="closeWorkbuddyAdd()">Close</button>
  </div>
  <div class="wb-dialog-body">
    <div class="form-row" style="margin-bottom:8px">
      <div class="field">
        <label>Realm</label>
        <select id="wbRealm"><option value="cn">CN (China)</option><option value="intl">International (codebuddy.ai)</option><option value="workbuddy">WorkBuddy (workbuddy.ai)</option></select>
      </div>
    </div>
    <div class="hint" id="wbAddStatus">Choose a realm and request an authorization link.</div>
    <div class="wb-link-row">
      <input type="text" id="wbOAuthUrl" readonly placeholder="authorization URL">
      <button class="btn btn-sm" onclick="copyWorkbuddyOAuthUrl()">Copy</button>
      <button class="btn btn-sm btn-primary" onclick="openWorkbuddyOAuthUrl()">Open</button>
    </div>
    <div class="wb-dialog-actions">
      <button class="btn btn-primary" id="wbStartLogin">Get authorization link</button>
      <button class="btn" id="wbImportJson" onclick="document.getElementById('wbDialogImportFile').click()">Import JSON</button>
    </div>
    <input type="file" id="wbDialogImportFile" accept=".json" hidden onchange="importWorkbuddyAccounts(this)">
  </div>
</dialog>

<dialog id="wbDetailsDialog" class="wb-wide">
  <div class="wb-dialog-head">
    <span>Points breakdown <span class="auto-pill" id="wbDetailsWho"></span></span>
    <button class="btn btn-sm" onclick="closeWorkbuddyDetails()">Close</button>
  </div>
  <div class="wb-dialog-body">
    <div class="hint" id="wbDetailsStatus">Loading...</div>
    <div id="wbDetailsBody"></div>
  </div>
</dialog>

<dialog id="accAddDialog">
  <div class="wb-dialog-head">
    <span>Add account</span>
    <button class="btn btn-sm" onclick="closeAccAddDialog()">Close</button>
  </div>
  <div class="wb-dialog-body">
    <p class="hint" style="margin-top:0" id="accAddHint">The type is detected automatically — sk_... is pooled as a static API key, anything else as an OAuth refresh token.</p>
    <div class="field">
      <label>Token or API key *</label>
      <input type="text" id="accAddToken" placeholder="workos-... refreshToken or sk_... API key" style="font-family:'JetBrains Mono',Consolas,monospace">
    </div>
    <div class="field">
      <label>Email (optional)</label>
      <input type="text" id="accAddEmail" placeholder="user@example.com">
    </div>
    <div class="wb-dialog-actions">
      <button class="btn btn-primary" id="accAddSubmit" onclick="accAddFromDialog()">Add account</button>
      <button class="btn" onclick="closeAccAddDialog()">Cancel</button>
    </div>
  </div>
</dialog>

<dialog id="zenKeyAddDialog">
  <div class="wb-dialog-head">
    <span>Add credentials</span>
    <button class="btn btn-sm" onclick="closeZenKeyAddDialog()">Close</button>
  </div>
  <div class="wb-dialog-body">
    <p class="hint" style="margin-top:0" id="zenKeyAddHint">One OpenCode API key per line (oc_sk_...).</p>
    <div class="field">
      <label>API keys (one per line) *</label>
      <textarea id="zenKeyAddInput" rows="5" placeholder="oc_sk_..." style="font-family:'JetBrains Mono',Consolas,monospace"></textarea>
    </div>
    <div class="wb-dialog-actions">
      <button class="btn btn-primary" id="zenKeyAddSubmit" onclick="zenKeyAddFromDialog()">Save</button>
      <button class="btn" onclick="closeZenKeyAddDialog()">Cancel</button>
    </div>
  </div>
</dialog>

<dialog id="orKeyAddDialog">
  <div class="wb-dialog-head">
    <span>Add OpenRouter keys</span>
    <button class="btn btn-sm" onclick="closeOrKeyAddDialog()">Close</button>
  </div>
  <div class="wb-dialog-body">
    <p class="hint" style="margin-top:0" id="orKeyAddHint">One OpenRouter API key per line (sk-or-v1-…). New keys start with routing disabled — tick Pool to enable.</p>
    <div class="field">
      <label>API keys (one per line) *</label>
      <textarea id="orKeyAddInput" rows="5" placeholder="sk-or-v1-…" style="font-family:'JetBrains Mono',Consolas,monospace"></textarea>
    </div>
    <div class="wb-dialog-actions">
      <button class="btn btn-primary" id="orKeyAddSubmit" onclick="orKeyAddFromDialog()">Save</button>
      <button class="btn" onclick="closeOrKeyAddDialog()">Cancel</button>
    </div>
  </div>
</dialog>

<dialog id="amdKeyAddDialog">
  <div class="wb-dialog-head">
    <span>Add AMDCloud keys</span>
    <button class="btn btn-sm" onclick="closeAmdKeyAddDialog()">Close</button>
  </div>
  <div class="wb-dialog-body">
    <p class="hint" style="margin-top:0" id="amdKeyAddHint">One AMDCloud API key per line (rc-…, issued at the <a href="https://developer.amd.com.cn/radeon/tokenfactory" target="_blank" rel="noopener" style="color:var(--accent)">Token Factory</a>). New keys start with routing disabled — tick Pool to enable.</p>
    <div class="field">
      <label>API keys (one per line) *</label>
      <textarea id="amdKeyAddInput" rows="5" placeholder="rc-…" style="font-family:'JetBrains Mono',Consolas,monospace"></textarea>
    </div>
    <div class="wb-dialog-actions">
      <button class="btn btn-primary" id="amdKeyAddSubmit" onclick="amdKeyAddFromDialog()">Save</button>
      <button class="btn" onclick="closeAmdKeyAddDialog()">Cancel</button>
    </div>
  </div>
</dialog>

<dialog id="thKeyAddDialog">
  <div class="wb-dialog-head">
    <span>Add TokenHarbor keys</span>
    <button class="btn btn-sm" onclick="closeTHKeyAddDialog()">Close</button>
  </div>
  <div class="wb-dialog-body">
    <p class="hint" style="margin-top:0" id="thKeyAddHint">One TokenHarbor universal key per line (thk_live_…, from the <a href="https://tokenharbor.ai/dashboard/api-keys" target="_blank" rel="noopener" style="color:var(--accent)">dashboard</a>). New keys start with routing disabled — tick Pool to enable.</p>
    <div class="field">
      <label>API keys (one per line) *</label>
      <textarea id="thKeyAddInput" rows="5" placeholder="thk_live_…" style="font-family:'JetBrains Mono',Consolas,monospace"></textarea>
    </div>
    <div class="wb-dialog-actions">
      <button class="btn btn-primary" id="thKeyAddSubmit" onclick="thKeyAddFromDialog()">Save</button>
      <button class="btn" onclick="closeTHKeyAddDialog()">Cancel</button>
    </div>
  </div>
</dialog>

<dialog id="traeAddDialog" style="width:min(560px,calc(100vw - 32px))">
  <div class="wb-dialog-head">
    <span>Add TraeWork accounts</span>
    <button class="btn btn-sm" onclick="closeTraeAddDialog()">Close</button>
  </div>
  <div class="wb-dialog-body">
    <details class="trae-acc" id="traeAccWeb" open>
      <summary>Web authorization</summary>
      <div class="trae-acc-body">
        <p class="hint" style="margin:0 0 8px" id="traeOAuthHint">Download the helper from this panel, run "python web_login.py" locally, then authorize on the opened page (sign in to trae.cn in the browser first). The account lands here automatically with routing off. The helper writes to the panel it was downloaded from.</p>
        <div style="display:flex;gap:8px;margin-bottom:6px;align-items:center">
          <a class="btn btn-sm btn-static" href="/trae/oauth/helper" download="web_login.py">Download helper</a>
          <button class="btn btn-sm btn-static" onclick="openTraeHelperPage()" title="Open the helper page (127.0.0.1:8765) and click the authorize button there">Open helper page</button>
        </div>
        <div class="hint" id="traeHelperStatus" style="margin:0">Checking local helper…</div>
      </div>
    </details>
    <details class="trae-acc" id="traeAccManual">
      <summary>Manual paste</summary>
      <div class="trae-acc-body">
        <p class="hint" style="margin:0 0 8px" id="traeAddHint">One Cloud-IDE-JWT (a 3-part JWT) per line. New accounts start with routing off — tick the Gateway switch to enable.</p>
        <div class="field">
          <label>Tokens (one per line)</label>
          <textarea id="traeAddInput" rows="5" placeholder="eyJhbGciOi…" style="font-family:'JetBrains Mono',Consolas,monospace"></textarea>
        </div>
      </div>
    </details>
    <div class="wb-dialog-actions">
      <button class="btn btn-primary" id="traeAddSubmit" onclick="traeAddFromDialog()">Save</button>
      <button class="btn" onclick="closeTraeAddDialog()">Cancel</button>
    </div>
  </div>
</dialog>

<dialog id="impKeepDialog" style="width:min(430px,calc(100vw - 32px))">
  <div class="wb-dialog-head">
    <span>Import config</span>
    <button class="btn btn-sm" onclick="closeImpKeep()">Close</button>
  </div>
  <div class="wb-dialog-body">
    <div class="hint" style="margin-bottom:10px">Import will replace ALL persisted config with the file contents. Confirm whether to keep each platform's credential enabled states.</div>
    <div style="display:flex;gap:8px;margin-bottom:12px">
      <button class="btn btn-sm" onclick="impKeepSetAll('impKeepRows',false)">Select all</button>
      <button class="btn btn-sm" onclick="impKeepSetAll('impKeepRows',true)">Invert</button>
    </div>
    <div id="impKeepRows" style="display:flex;flex-direction:column;gap:10px;margin-bottom:14px"></div>
    <div style="display:flex;gap:8px;justify-content:flex-end">
      <button class="btn" onclick="closeImpKeep()">Cancel</button>
      <button class="btn btn-primary" onclick="confirmImpKeep()">Confirm import</button>
    </div>
  </div>
</dialog>
<dialog id="impReorderDialog" style="width:min(430px,calc(100vw - 32px))">
  <div class="wb-dialog-head">
    <span>Reorder by latency</span>
    <button class="btn btn-sm" onclick="closeReorderDlg()">Close</button>
  </div>
  <div class="wb-dialog-body">
    <div class="hint" style="margin-bottom:10px">re-bind proxies of the selected platforms: healthy (fast, allowed) bindings stay put; slow/unrecorded/excluded/over-cap ones are re-filled from the remaining pool (better-only for healthy rows), then auto-save.</div>
    <div style="display:flex;gap:8px;margin-bottom:12px">
      <button class="btn btn-sm" onclick="impKeepSetAll('impReorderRows',false)">Select all</button>
      <button class="btn btn-sm" onclick="impKeepSetAll('impReorderRows',true)">Invert</button>
    </div>
    <div id="impReorderRows" style="margin-bottom:14px"></div>
    <div style="display:flex;gap:8px;justify-content:flex-end">
      <button class="btn" onclick="closeReorderDlg()">Cancel</button>
      <button class="btn btn-primary" onclick="confirmReorder()">Confirm</button>
    </div>
  </div>
</dialog>
<dialog id="otherPlatformDialog">
  <div class="wb-dialog-head">
    <span>New sub-platform</span>
    <button class="btn btn-sm" onclick="closeOtherPlatformDialog()">Close</button>
  </div>
  <div class="wb-dialog-body">
    <p class="hint" style="margin-top:0">Create an entry for an upstream provider: shared Base URL, its API keys, and the model prefix its models are exposed under.</p>
    <div class="field">
      <label>Name *</label>
      <input type="text" id="opName" placeholder="Acme Cloud">
    </div>
    <div class="field">
      <label>Model prefix (required, unique) *</label>
      <input type="text" id="opPrefix" placeholder="acme" style="font-family:'JetBrains Mono',Consolas,monospace">
    </div>
    <div class="field">
      <label>Base URL</label>
      <input type="text" id="opBaseURL" placeholder="https://api.acme.com/v1" style="font-family:'JetBrains Mono',Consolas,monospace">
    </div>
    <div class="wb-dialog-actions">
      <button class="btn btn-primary" onclick="doOtherPlatformCreate()">Create</button>
      <button class="btn" onclick="closeOtherPlatformDialog()">Cancel</button>
    </div>
  </div>
</dialog>

<dialog id="otherManageDialog">
  <div class="wb-dialog-head">
    <span>Manage sub-platforms</span>
    <button class="btn btn-sm" onclick="closeOtherManageDialog()">Close</button>
  </div>
  <div class="wb-dialog-body">
    <p class="hint" style="margin-top:0">Drag a row to change the order; the order decides how sub-platforms appear in the selector.</p>
    <div id="otherManageList"></div>
    <div class="wb-dialog-actions">
      <button class="btn btn-primary" onclick="saveOtherPlatformOrder()">Save order</button>
      <button class="btn" onclick="closeOtherManageDialog()">Cancel</button>
    </div>
  </div>
</dialog>

<dialog id="otherKeyAddDialog">
  <div class="wb-dialog-head">
    <span>Add API keys</span>
    <button class="btn btn-sm" onclick="closeOtherKeyAddDialog()">Close</button>
  </div>
  <div class="wb-dialog-body">
    <p class="hint" style="margin-top:0" id="otherKeyAddHint">One API key per line for this sub-platform. New keys start with routing disabled — tick Pool to enable.</p>
    <div class="field">
      <label>API keys (one per line) *</label>
      <textarea id="otherKeyAddInput" rows="5" placeholder="sk-…" style="font-family:'JetBrains Mono',Consolas,monospace"></textarea>
    </div>
    <div class="wb-dialog-actions">
      <button class="btn btn-primary" onclick="otherKeyAddFromDialog()">Save</button>
      <button class="btn" onclick="closeOtherKeyAddDialog()">Cancel</button>
    </div>
  </div>
</dialog>

<dialog id="wbTodoDialog" class="wb-wide">
  <div class="wb-dialog-head">
    <span>Task center&#x20;</span>
    <button class="btn btn-sm" onclick="closeWorkbuddyTodoScan()">Close</button>
  </div>
  <div class="wb-dialog-body">
    <div class="justify-between">
      <div class="hint" id="wbTodoStatus">Pick a concurrency, then click "Scan tasks"</div>
      <div class="wb-task-actions">
        <label class="inline-flex" style="font-size:12px;color:var(--text2)">Concurrency
          <select id="wbTodoConcurrency" style="width:64px">
            <option value="1" selected>1</option>
            <option value="2">2</option>
            <option value="3">3</option>
          </select>
        </label>
        <button class="btn btn-sm" id="wbTodoScan" onclick="scanWorkbuddyTodos()">Scan tasks</button>
        <button class="btn btn-sm btn-primary" id="wbTodoRunAll" onclick="runWorkbuddyAllTodos()">Run all</button>
      </div>
    </div>
    <div id="wbTodoBody" style="margin-top:12px"></div>
  </div>
</dialog>

<script>
const API = '/admin/api';

// ========== Theme toggle ==========
function getTheme() {
  return localStorage.getItem('theme') || 'dark';
}
function applyTheme(t) {
  if (t === 'dark') {
    document.documentElement.setAttribute('data-theme', 'dark');
    const ic = document.getElementById('themeIcon');
    if (ic) ic.innerHTML = '<svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></svg>';
  } else {
    document.documentElement.setAttribute('data-theme', 'light');
    const ic = document.getElementById('themeIcon');
    if (ic) ic.innerHTML = '<svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="4"/><path d="M12 2v2m0 16v2M4.9 4.9l1.4 1.4m11.4 11.4 1.4 1.4M2 12h2m16 0h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/></svg>';
  }
}
function toggleTheme() {
  const cur = getTheme();
  const next = cur === 'dark' ? 'light' : 'dark';
  localStorage.setItem('theme', next);
  applyTheme(next);
}
applyTheme(getTheme());

const _ = id => document.getElementById(id);
const esc = s => String(s ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const fmtNum = n => (n || 0).toLocaleString('en-US');
// fmtWhen 把服务器发来的 RFC3339 时刻渲染成浏览器的本地时间（同一时刻在不同
// 时区看到各自的钟点）。容器时区是 UTC，直接显示服务器格式化的读数会和本地
// 时间差一个时差；解析失败则原样返回，至少不丢信息。
// 零值时间（Go 的 time.Time{} = 0001-01-01T00:00:00Z）视为"没有这个时刻"：
// 它是 truthy，会被 new Date 解析成年份 1 并渲染成 "1/1/1, 12:00:00 AM"。
const fmtWhen = s => {
  if (!s || String(s).startsWith('0001-')) return '';
  const d = new Date(s);
  return isNaN(d.getTime()) ? s : d.toLocaleString('en-US');
};
// fmtShort 紧凑时刻（MM-DD HH:mm）：表格窄列（冷却中/轮换周期/恢复提示）用，
// 避免完整 toLocaleString 长串撑宽挤压相邻列；完整读数放 title 悬浮。
const fmtShort = s => {
  if (!s || String(s).startsWith('0001-')) return '';
  const d = new Date(s);
  if (isNaN(d.getTime())) return String(s);
  const p = n => String(n).padStart(2, '0');
  return (d.getMonth() + 1) + '-' + p(d.getDate()) + ' ' + p(d.getHours()) + ':' + p(d.getMinutes());
};
// fmtLastUsed 凭据表「最近使用」列的紧凑时刻：去年份（en-US 完整串
// 10/27/2026, 4:29:53 PM → 10/27 4:29:53 PM），避免年份撑宽窄列；
// 解析失败/零值行为与 fmtWhen 一致（2026-10-27 用户定）。
const fmtLastUsed = s => {
  if (!s || String(s).startsWith('0001-')) return '';
  const d = new Date(s);
  if (isNaN(d.getTime())) return String(s);
  const p = n => String(n).padStart(2, '0');
  // 24 小时制（2026-10-27 用户定）：10/27 18:29:53
  return (d.getMonth() + 1) + '/' + d.getDate() + ' ' + p(d.getHours()) + ':' + p(d.getMinutes()) + ':' + p(d.getSeconds());
};
const fmtTokens = n => {
  n = n || 0;
  if (n >= 1000000) return (n / 1000000).toFixed(2).replace(/\.?0+$/, '') + 'M';
  if (n >= 1000) return (n / 1000).toFixed(1).replace(/\.0$/, '') + 'K';
  return String(n);
};
// keyTokensCell/keyLastUsedCell：key 池 token 记账两列（与 cline 页同格式）。
// tokensToday/tokensTotal 缺失 = 老数据，未记账显示 -。
const keyTokensCell = k => {
  if (k.tokensToday == null && k.tokensTotal == null) return '<td style="text-align:center;color:var(--text3)">-</td>';
  return '<td title="' + T('Today') + ' ' + fmtNum(k.tokensToday) + ' / ' + T('total') + ' ' + fmtNum(k.tokensTotal) + ' ' + T('tokens (exact when upstream returns usage, otherwise estimated)') + '">' + fmtTokens(k.tokensToday) + ' / ' + fmtTokens(k.tokensTotal) + '</td>';
};
const keyLastUsedCell = k => '<td class="mono" style="font-size:11px">' + (k.lastUsed ? esc(fmtLastUsed(k.lastUsed)) : '-') + '</td>';
// ========== i18n（English / 简体中文） ==========
// 机制：字典以英文原文为键。静态 HTML 与 JS 渲染出的纯文本节点由
// MutationObserver 即时翻译（渲染代码无需逐处包翻译函数）；与变量拼接的
// 字符串在渲染处用 T() 包住英文片段；confirm() 原生对话框只能显式 T()。
// 切换语言 = 保存 localStorage 偏好 + location.reload()（避免维护反向映射，
// 也保证重载后静态文本从首次渲染起就是目标语言）。
let LANG = localStorage.getItem('lang') === 'zh' ? 'zh' : 'en';
const I18N_ZH = {
  // WorkBuddy native admin console
  'Original panel': '原版入口',
  'Task center': '任务中心',
  'Refresh all': '全部刷新',
  'Refresh all account balances': '刷新全部账号的余额',
  'Check in all': '全部签到',
  'Pet patrol': '宠物巡检',
  'Activity all': '连登活跃',
  'Refresh login': '刷新登录',
  'Refresh': '刷新',
  'Account': '账号',
  'Points': '积分',
  'Import JSON': '导入 JSON',
  'Failed to load WorkBuddy accounts: ': '加载 WorkBuddy 账号失败：',
  'Credits': '额度',
  'Check-in': '签到',
  'Disabled': '已停用',
  'Deleted': '已删除',
  'accounts': '账号',
  'tok': 'token',
  'Get authorization link': '获取授权链接',
  'Choose a realm and request an authorization link.': '选择区域并请求授权链接。',
  'Copy failed': '复制失败',
  // 拖拽排序
  'Save or discard staged edits before reordering': '有未保存的暂存修改，请先保存或放弃后再拖拽排序',
  'Order saved': '顺序已保存',
  'Reorder failed: ': '排序失败：',
  'loaded': '已加载',
  'load failed': '加载失败',
  'WorkBuddy config load failed: ': 'WorkBuddy 配置加载失败：',
  'WorkBuddy config saved': 'WorkBuddy 配置已保存',
  'WorkBuddy config saved (': 'WorkBuddy 配置已保存 (',
  ' restart items)': ' 个重启项)',
  'WorkBuddy config save failed: ': 'WorkBuddy 配置保存失败：',
  'WorkBuddy': 'WorkBuddy',
  'Add WorkBuddy account': '添加 WorkBuddy 账号',
  'Close': '关闭',
  'Realm': '区域',
  'authorization URL': '授权 URL',
  'Copy': '复制',
  // 侧边导航 / 分组
  'Account pool': '账号池',
  'Services': '服务',
  'Dashboard': '仪表盘',
  'Gateway settings': '网关设置',
  'Request logs': '请求日志',
  'Proxy pool': '代理池',
  'Custom aliases': '自定义别名',
  'Light': '亮色',
  'Dark': '暗色',
  'Toggle theme': '切换主题',
  // 仪表盘
  'Platform overview': '平台概览',
  'Request statistics': '请求统计',
  'Counted since ': '统计自 ',
  'requests': '请求',
  'errors': '错误',
  'Avg ': '平均 ',
  'Cooldown': '冷却中',
  'Add account': '添加账号',
  'Endpoints': '接口',
  'OpenAI Chat format — works with most tools out of the box': 'OpenAI Chat 格式 —— 大多数工具开箱即用',
  'Anthropic Messages format — for Claude Code / Cline and similar clients': 'Anthropic Messages 格式 —— 供 Claude Code / Cline 等客户端使用',
  'OpenAI Responses format — for Cursor and similar clients': 'OpenAI Responses 格式 —— 供 Cursor 等客户端使用',
  'Model list': '模型列表',
  // 账号页
  'Add': '添加帐号',
  'Test all': '批量测试',
  'Disable all': '一键禁用',
  'Bind proxies in order': '一键代理',
  'Bind main proxy by row order: 1st credential to Direct, then the proxy pool in order (staged - click Save to apply; enable/disable and backup untouched)': '按行序依次绑定主代理：第1个凭据绑直连，之后依次对应代理池（仅暂存，点保存才生效；不影响启用状态与备用代理）',
  'No credentials to bind': '当前页没有可绑定的凭据',
  'Proxy pool is empty': '代理池为空，无法按序绑定',
  'Already bound in this order': '当前已是该顺序，无需改动',
  'Staged ': '已暂存 ',
  ' proxy binding(s) — click Save to apply': ' 条代理绑定，点保存生效',
  '; ': '；',
  ' row(s) left unchanged (no proxy left)': ' 行凭据无对应代理，保持原样',
  'Reorder by latency': '凭据延迟重排',
  // 代理排除规则（代理池页：平台级代理黑名单 + 每代理绑定上限；原「自动分配规则」）
  'Proxy exclusion rules': '代理排除规则',
  'Save proxy exclusion rules': '保存代理排除规则',
  'Saved proxy exclusion rules': '代理排除规则已保存',
  'Proxy exclusion rules: ': '代理排除规则：',
  'Batch-append': '批量追加',
  'Clear selection': '清空选择',
  'Nothing to append': '没有需要追加的代理',
  'Appended to every platform\'s exclusion list (staged) — click Save to apply': '已追加进所有平台的排除列表（暂存）——点「保存」后生效',
  'Click to pick platforms to ban…': '点击选择要封禁的平台…',
  'Platform': '平台',
  'Excluded proxies': '排除的代理',
  'Exclude Direct': '排除直连',
  'Add excluded proxy': '添加排除代理',
  'No pool proxies left to exclude': '没有可添加的池内代理（都已排除或池为空）',
  'Add proxy': '添加',
  'Remove': '移除',
  'Nothing excluded': '未排除',
  'Max reuse per proxy': '代理最多复用',
  'times': '次',
  'Search and add a proxy…': '搜索并添加代理…',
  'No match': '无匹配代理',
  ' sub-platform(s) · each configured separately': ' 个子平台 · 各自独立配置',
  'No sub-platform yet': '暂无子平台',
  'Reorder credential proxies by latency': '凭据代理延迟重排',
  'Proxy isolation: each credential only egresses via its bound exits; when none is available it is skipped — never another exit or direct. Forced on when': '代理隔离：每个凭据只从绑定的出口出站，都不可用则跳过——绝不改走其他出口或直连。设有',
  '=true.': '=true 时强制开启。',
  'Backup proxy — off by default: every identity uses its main proxy only; a cooling or removed main proxy is skipped for that round. On: the backup proxy serves as the fallback for the main one.': '备用代理：默认关闭——每个身份只走主代理，主代理冷却或被移除时直接跳过。开启后：备用代理作为主代理的兜底。',
  'Platform-level blacklist and per-proxy binding cap for automatic assignment only — one-click bind and reorder-by-latency. Manual bindings and runtime routing are never restricted.': '仅作用于自动分配——「一键代理」与「凭据延迟重排」两个批量操作；手动绑定和运行期路由不受限制。',
  'Excluded': '已排除',
  ' · excluded': ' · 已排除',
  ' row(s) left unchanged — every allowed proxy is excluded or at cap': ' 行无法改绑：可用代理都已拉黑或达到上限',
  're-bind proxies of the selected platforms: healthy (fast, allowed) bindings stay put; slow/unrecorded/excluded/over-cap ones are re-filled from the remaining pool (better-only for healthy rows), then auto-save.': '重排所选平台的代理绑定：健康绑定（快代理且未拉黑/未超限）保持不动；慢/无记录/被拉黑/超限的行从剩余代理池回填（健康行只在更优时才换），并自动保存。',
  'Re-bind proxies of all credentials (except Direct/Global) ordered by measured latency, then auto-save.': '按实测延迟重排全部凭据（直连/全局除外）的代理绑定并自动保存。',
  'Latency reorder finished with errors — loads failed: ': '延迟重排完成但有错误——加载失败：',
  '; saves failed: ': '；保存失败：',
  'Reordered ': '已按延迟重排 ',
  ' proxy binding(s) by latency and saved': ' 条代理绑定并已保存',
  'No changes to save': '没有需要保存的修改',
  'Save failed for ': '保存失败 ',
  ' account(s)': ' 个账号',
  ' account change(s)': ' 处修改',
  'No accounts to test': '没有可测试的账号',
  'Batch test done: ': '批量测试完成：',
  ' tested, ': ' 个已测，',
  ' ok': ' 可用',
  ' cooling': ' 冷却',
  ' error': ' 异常',
  'Participates in request routing': '参与请求路由',
  'auto': '自动',
  'The alias (#name) is a proxy\'s identity — re-binding survives alias-preserving URL changes.': '别名（#name）是代理的身份标识——URL 变化但别名不变时，绑定关系保持有效。',
  'Rows follow the backend pool; values persist in this browser to survive pool edits (add-only — a returning proxy picks up its stored numbers). Console override: proxyRateSet(\'香港-01\', 0.42).': '行列表跟随代理池后端；数值在本浏览器留存以扛住池编辑清零（只加不删，代理回归后自动恢复留存值）。控制台改数：proxyRateSet(\'香港-01\', 0.42)。',
  'Import will replace ALL persisted config with the file contents. Confirm whether to keep each platform\'s credential enabled states.': '导入将替换所有持久化配置，请确认是否保留各平台凭据启用状态。',
  'International (codebuddy.ai)': '国际版（codebuddy.ai）',
  'WorkBuddy (workbuddy.ai)': 'WorkBuddy 桌面版（workbuddy.ai）',
  'Discard': '复位',
  'Discard staged changes and reload the last saved state': '撤销暂存的修改，恢复为上次保存的状态',
  'Add credentials': '添加凭据',
  'Confirm': '确认',
  'keys': '个 key',
  'API keys (one per line) *': 'API key（每行一个）*',
  'One OpenCode API key per line (oc_sk_...).': '每行一个 OpenCode API key（oc_sk_...）。',
  'Enter at least one key': '请至少输入一个 key',
  'No new keys (all already in the pool)': '没有新 key（均已在池中）',
  'Added ': '已添加 ',
  ' key(s)': ' 个 key',
  'No keys to test': '没有可测试的 key',
  ' (deleted)': '（已删除）',
  'Running': '执行中',
  'Finished': '执行结束',
  'CN (China)': 'CN (国内版)',
  'Global (International)': 'Global (国际版)',
  'Points breakdown': '积分构成',
  'Pick a concurrency, then click "Scan tasks"': '选择并发数后点击“扫描任务”',
  'Concurrency': '并发',
  'Scan tasks': '扫描任务',
  'Run all': '全部执行',
  'Main egress: requests go through this egress whenever available': '主出口：可用时固定从该出口发出',
  'Backup egress: fallback while the main egress is cooling down or unavailable': '备用出口：主出口冷却或不可用时兜底',
  'Failed to save ': '保存失败 ',
  ' account edits': ' 个账号的修改',
  'Clear proxy bindings on ALL accounts and disable them? Takes effect when you click Save.': '清空全部账号的代理绑定并置为禁用？点击"保存"后生效。',
  'Delete this WorkBuddy account and its credentials?': '删除该 WorkBuddy 账号及其凭证？',
  '(unnamed)': '(未命名)',
  'Querying points breakdown from upstream...': '正在向上游查询积分构成...',
  'No points data found for this account': '未找到该账号的积分数据',
  'Live from upstream · ': '实时查询上游 · ',
  ' points packages': ' 个积分包',
  'Failed to read: ': '读取失败：',
  'Query failed: ': '查询失败：',
  'first issued ': '首发 ',
  'Total ': '共 ',
  ' package(s)': ' 个包',
  'Remaining points · used ': '剩余积分 · 已用 ',
  'Package / source': '包名 / 来源',
  'Face value': '面额',
  'Remaining': '剩余',
  'Used': '已用',
  'Granted': '发放',
  'Expires': '到期',
  'No points packages': '没有积分包',
  'No pending tasks for all accounts': '全部账号没有待办任务',
  'Energy': '能量',
  'Task: ': '任务：',
  'Description: ': '说明：',
  'Progress: ': '进度：',
  'Reward: ': '奖励：',
  'Result: ': '结果：',
  'Pending': '待执行',
  'Scan failed: ': '扫描失败：',
  ' item(s)': ' 项',
  'Scanning with concurrency ': '正在按并发 ',
  ' scanning tasks...': ' 扫描任务...',
  'Scanning...': '扫描中...',
  'Scan complete: ': '扫描完成：',
  ' pending item(s)': ' 项待办',
  'Claimed': '已领取',
  'In progress': '进行中',
  'Done': '完成',
  'Skipped': '跳过',
  'Failed': '失败',
  ': ': '：',
  'Failed to read queue: ': '读取队列失败：',
  'Run all listed tasks with concurrency ': '按并发 ',
  ' (tasks run serially within each account, auto-claim enabled). Confirm?': ' 执行列表中的全部任务（账号内串行，自动领取任务）。确认继续？',
  'No pending tasks': '没有待办任务',
  'Queue started: ': '队列已启动：',
  ' item(s) (concurrency ': ' 项（并发 ',
  'Queue started, running...': '队列已启动，正在执行...',
  'Failed to run: ': '执行失败：',
  'Saving...': '保存中...',
  'free': '免费',
  'Default tier': '默认档',
  'No WorkBuddy models under the current filter': '当前筛选下没有 WorkBuddy 模型',
  'Model rate filter': '模型倍率筛选',
  '0 — free only': '0 — 仅免费',
  'WorkBuddy configuration': 'WorkBuddy 配置',
  'Managed by the admin session': '面板由当前管理员会话管理',
  'Scheduled jobs': '定时任务',
  'Automatic check-in': '自动签到',
  'Check-in hours': '签到时点（小时，逗号分隔）',
  'Automatic patrol': '自动巡检',
  'Patrol hours': '旅行时点（小时，逗号分隔）',
  'Automatic activity': '自动连登',
  'Activity hours': '上报时点（小时，逗号分隔）',
  'Automatic keepalive': '自动保活',
  'Keepalive hours': '保活时点（小时，逗号分隔）',
  'Background balance refresh': '后台刷新余额',
  'Refresh interval (min)': '刷新间隔（分钟）',
  'Growth tasks': '成长任务自动执行',
  'Growth hours': '执行时点（小时，逗号分隔）',
  'Pool and cooldown': '账号池与流量治理',
  'Max in-flight / account': '单账号最大在途',
  'Global in-flight limit': '国际版在途上限',
  'Breaker threshold': '连续失败熔断阈值',
  'Soft rate cooldown': '软限流冷却基数',
  'Soft cooldown max': '软冷却退避上限',
  'Breaker cooldown': '熔断基础时长',
  'Breaker cooldown max': '熔断退避上限',
  'Degrade threshold': '连败降权阈值',
  'Degrade cooldown': '连败降权时长',
  'Degrade cooldown max': '连败降权上限',
  'Idle weight / hour': '闲置补偿 / 小时',
  'Idle weight max': '闲置补偿上限',
  'Cost explore window': '成本探索窗口',
  'Sticky TTL': '会话粘性 TTL',
  'Session and upstream': '上游与高级',
  'Short request timeout': '短请求超时',
  'First-byte timeout': '聊天首字节超时',
  'Stream idle timeout': '流空闲超时',
  'User-Agent': '出站 User-Agent',
  'Leave empty = CLI / 2.171.0 CodeBuddy / 2.171.0': '留空 = CLI/2.171.0 CodeBuddy/2.171.0',
  'System prompt mode': '系统提示词模式',
  'custom — gateway-owned prompt (avoids fingerprint false positives)': 'custom — 网关自有提示词（避免指纹误报）',
  'append — insert gateway prompt after client system (both apply)': 'append — 客户端 system 后插网关提示词（并用）',
  'passthrough — pass client system through unchanged': 'passthrough — 透传客户端原始 system',
  'Prompt file': '提示词文件路径',
  'Leave empty = built-in default prompt': '留空 = 内置默认提示词',
  'Sanitize outbound fingerprints': '出站请求指纹脱敏',
  'Sticky session routing': '会话粘性路由',
  'Upstash Redis mirror, auth dir, and state file are edited in the config file manually (restart items).': 'Upstash Redis 镜像、凭证目录与状态文件路径需手工编辑配置文件（判为重启项）。',
  'Discard changes': '放弃修改',
  'Save configuration': '保存配置',
  ' (was: ': '（原状态：',
  'Reset failed: ': '恢复失败：',
  'Key #': 'key #',
  ' (': '（',
  ') — ': '）— ',
  'cooldown until ': '冷却至 ',
  ' (seed)': '（种子）',
  ' failed': ' 失败',
  'Email (optional)': '邮箱（可选）',
  'Cancel': '取消',
  'Edit nickname': '修改昵称',
  'Nickname': '昵称',
  'Nickname saved': '昵称已保存',
  'Failed to save nickname: ': '昵称保存失败：',
  'Nickname cannot be empty': '昵称不能为空',
  'The type is detected automatically — sk_... is pooled as a static API key, anything else as an OAuth refresh token.': '类型自动识别：sk_... 按静态 API key 入池，其余按 OAuth refreshToken 处理。',
  ', ': '，',
  'Clear proxy bindings on ALL keys and disable routing? Click Save to apply.': '清空全部 key 的代理绑定并禁用路由？点击「保存」后生效。',
  'Stage only — click Save to apply. Clears bindings and disables every row.': '仅暂存——点「保存」后生效。清空所有行的代理绑定并禁用。',
  'Run the Test probe on every row.': '对本页所有行执行一次测试探测。',
  'Email': '邮箱',
  'Today/Total tokens': '今日/累计 token',
  'Last used': '最近使用',
  'Created': '创建时间',
  'Proxy binding': '代理绑定',
  'Actions': '操作',
  // WorkBuddy 账号表双开关列（账号/反代，afree）
  'Account switch': '账号开关',
  'Gateway switch': '反代开关',
  'Account master switch. Off = the account does not exist: check-in, patrol, activity, keepalive, night chats, growth and balance refresh all skip it, and it never routes.': '账号总开关。关闭 = 该账号不存在：签到、旅行、活跃、保活、夜猫子、成长任务与余额刷新全部跳过，也不再参与路由。',
  'Gateway (reverse proxy) switch: unchecked = model sessions never route to this account; check-in and all other maintenance tasks are unaffected. Can only be ticked while the account switch is on.': '反代开关：不勾选 = 模型会话不路由到该账号；签到等其余维护任务不受影响。只有账号开关开启时才能勾选。',
  'Account master switch: off = the account does not exist — check-in, tasks and balance refresh all stop, and it never routes': '账号总开关：关闭 = 该账号不存在——签到、任务与余额刷新全部停止，也不再路由',
  'Gateway switch: unchecked = model sessions never route to this account; check-in and all other maintenance tasks are unaffected': '反代开关：不勾选 = 模型会话不路由到该账号；签到等其余维护任务不受影响',
  // 封禁平台（网关设置页，封禁模型区上方；整平台退出会话路由）
  'Banned platforms': '封禁平台',
  'Banned platforms stop participating in session routing — models leave /v1/models and requests fail immediately; maintenance tasks (check-in etc.) are unaffected. Other sub-platforms can be banned individually. Changes are staged — click Save to save.': '封禁后整平台退出会话路由——模型从 /v1/models 移除、调用立即失败；签到等维护任务不受影响。Other 的子平台可单独封禁。改动先暂存，点「保存」后统一落盘。',
  'Banned automatically (upstream refuses them for this account type, e.g. code 11158) or manually — hidden from /v1/models. Test sends a real request with the first enabled credential; Unblock only removes the entry, the same upstream error bans it again.': '自动封禁（上游按账号类型拒绝，如 code 11158）或手动封禁；已从 /v1/models 移除。Test 用该平台第一个启用的凭据发真实请求验证；「解禁」只移出名单，同样的上游错误会再次封禁。',
  // 封禁平台暂存 + 确认按钮（勾选/去选只改暂存，「确认」差集统一下发落盘）
  'Staged — click Save to save': '已暂存，点「保存」后落盘',
  'No changes to apply': '没有需要应用的改动',
  'Platform bans saved': '封禁平台已保存',
  // 批量追加框「已在所有平台排除」回显 chip 与批量取消
  'Excluded on every platform — × removes it from all (staged)': '已在所有平台排除——点 × 从所有平台批量取消（暂存）',
  'Removed from every platform\'s exclusion list (staged) — click Save to apply': '已从所有平台的排除列表移除（暂存）——点「保存」后生效',
  'Loading...': '加载中...',
  'Counted locally by this proxy — not the official free quota': '本代理本地统计 —— 非官方免费额度',
  'Main exit first, backup as fallback; both down = row skipped (isolation).': '主出口优先，备用兜底；都不可用即跳过该行（隔离模式）。',
  'Main exit — used whenever available': '主出口 —— 可用即优先',
  'Backup exit — used when main is cooling/removed': '备用出口 —— 主冷却/被删时使用',
  'Bound proxy is no longer in the proxy pool — skipped until fixed': '绑定的代理已不在代理池中——修复前跳过该行。',
  'session minted': '会话铸造于',
  // 添加账号弹窗（accAddDialog，Cline 页"添加账号"按钮）
  'Token or API key *': 'Token 或 API key *',
  // 代理池页
  'Egress proxies': '出口代理',
  'One proxy per line:': '每行一个代理：',
  '. All upstreams share this list; unhealthy proxies are skipped automatically.': '。所有上游共用此列表；不健康的代理会被自动跳过。',
  'The alias (#name) is a proxy\'s identity — re-binding survives alias-preserving URL changes.': '别名（#name）是代理的身份标识——URL 变化但别名不变时，绑定关系保持有效。',
  'When a subscription URL is set, this list is managed by the subscription and read-only; it serves as the fallback pool while a fetch fails.': '设置订阅链接后，此列表由订阅管理、只读不可编辑；拉取失败时作为兜底池使用。',
  'or': '或',
  'one per line: socks://b64(user:pass)@host:port#alias, socks5://user:pass@host:port#alias or http://host:port': '每行一条：socks://b64(账号:密码)@host:port#别名、socks5://账号:密码@host:port#别名 或 http://host:port（#别名自动识别）',
  'Proxy list': '代理列表',
  'Proxy selection strategy': '代理选择策略',
  'Round-robin (round_robin)': '轮询（round_robin）',
  'Random (random)': '随机（random）',
  'Fill (fill)': '填满优先（fill）',
  'Save proxy pool': '保存代理池',
  'Global policy': '全局策略',
  'Cline': 'Cline',
  'Use proxy pool': '走代理池',
  'Proxy': '代理',
  'Attempts': '尝试数',
  'Rate': '在线率',
  'Recent': '最近',
  'Last OK': '最近成功',
  'Last Fail': '最近失败',
  'Upstream attempts that egressed via this proxy since the pool was last edited': '上次编辑代理池以来，经此代理出网的上游尝试次数',
  'Success rate over the last 50 attempts; red below 80% (min 10 samples).': '最近 50 次尝试的在线率；低于 80% 标红（至少 10 个样本）。',
  'Average tunnel-setup time over successful attempts (TCP+TLS+CONNECT handshake only — excludes upstream first byte and task duration). Lower is better. Click to sort.': '成功尝试的平均隧道建立耗时（仅 TCP+TLS+CONNECT 握手——不含上游首字节与任务耗时）。越低越好。点击排序。',
  'Average tunnel-setup time over successful attempts (TCP+TLS+CONNECT handshake only — excludes upstream first byte and task duration). Lower is better.': '成功尝试的平均隧道建立耗时（仅 TCP+TLS+CONNECT 握手——不含上游首字节与任务耗时）。越低越好。',
  'Time to first byte: from request start to the first response byte (streaming = first upstream token; - = no response body, e.g. 413)': '首字节耗时：请求开始到首个响应字节（流式=上游首 token 到达；-=无响应体如 413）',
  'Avg tunnel-setup time of successful attempts (TCP+TLS+CONNECT), excluding upstream response; samples: ': '成功尝试的平均隧道建立耗时（TCP+TLS+CONNECT），不含上游响应；样本数 ',
  'Add API keys': '添加 API key',
  'All its keys will be removed. This cannot be undone.': '该子平台的所有密钥都将被移除，此操作不可撤销。',
  'Transport-layer success rate since the pool was last edited (upstream 4xx/5xx still counts as the proxy being online)': '上次编辑代理池以来的传输层成功率（上游 4xx/5xx 不算代理失败）',
  'Success rate over the last 50 attempts; red below 80% (min 10 samples).': '最近 50 次尝试的成功率；样本≥10 且低于 80% 时标红。',
  'Rates are sampled passively from real traffic — no probing. "No data" = unused since the last pool edit; editing the proxy list resets counters.': '在线率仅被动采样自真实流量——不产生探测流量。“无数据”=自上次编辑代理池后未使用；编辑代理列表会清零计数。',
  'Rows follow the backend pool; values persist in this browser to survive pool edits (add-only — a returning proxy picks up its stored numbers). Console override: proxyRateSet(\'香港-01\', 0.42).': '行列表跟随代理池后端；数值在本浏览器留存以扛住池编辑清零（只加不删，代理回归后自动恢复留存值）。控制台改数：proxyRateSet(\'香港-01\', 0.42)。',
  'Clear data': '清空数据',
  'Wipe this browser\'s stored online-rate rows (manual edits and proxies no longer in the pool); the table falls back to current backend data.': '清掉本浏览器留存的在线率行（手动改动与已不在池内的代理），表格回退到后端当前数据。',
  'Clear the browser-stored online-rate data? Backend stats are untouched.': '清空浏览器留存的在线率数据？后端统计不受影响。',
  'Cleared local online-rate data': '已清空本地在线率数据',
  'Restore defaults': '恢复默认',
  'Restore the default Cline CLI headers': '恢复默认的 Cline CLI 请求头',
  'Headers restored to defaults': '请求头已恢复默认',
  'OpenCode keys without proxy bindings follow this switch.': 'OpenCode 未绑定代理的 key 遵循此开关。',
  'Clear bindings': '清空代理',
  ' and ': ' 和 ',
  'Enable all': '一键启用',
  'Direct connection': '直连',
  'Save': '保存',
  'Cline accounts without proxy bindings follow this switch.': 'Cline 未绑定代理的账号遵循此开关。',
  'TraeWork accounts without proxy bindings follow this switch.': 'TraeWork 未绑定代理的账号遵循此开关。',
  'Delete': '删除',
  'Test': '测试',
  'Reset': '重置',
  'Open': '打开',
  'Admin panel:': '管理面板：',
  'API address:': 'API 地址：',
  'Config export / import': '配置导入导出',
  'Export config': '导出配置',
  'Import config': '导入配置',
  'Export contains credentials — keep the file safe. Import overwrites existing config.': '导出文件包含凭据——请妥善保管。导入会覆盖现有配置。',
  'Config exported': '配置已导出',
  'Config imported': '配置已导入',
  'Unsupported backup version': '不支持的备份版本',
  'Invalid backup file: ': '备份文件无效：',
  'Import replaces sections present in the file (accounts / keys / proxies / combos ...). All imported identities start DISABLED. Continue?': '导入将用文件内容替换当前对应的配置段（所有凭据、代理池、别名等，文件中存在的段都会替换）。导入的所有凭据一律禁用，需在面板逐个启用。继续？',
  'Import will replace ALL persisted config with the file contents. Confirm whether to keep each platform\'s credential enabled states.': '导入将替换所有持久化配置，请确认是否保留各平台凭据启用状态。',
  'Select all': '全选',
  'Invert': '反选',
  'No platform selected': '未选择任何平台',
  'Confirm import': '确认导入',
  'State': '状态',
  'Global': '全局',
  'No matching proxies': '没有匹配的代理',
  'Proxy isolation (identity ↔ exit binding)': '代理隔离（身份 ↔ 出口绑定）',
  'Isolation mode': '隔离模式',
  'Enabled (default) — bound identities only ever use their bound exits': '启用（默认）—— 绑定的身份只从绑定出口出网',
  'Disabled — ignore bindings; exits rotate per the strategy above (direct when pool off)': '关闭 —— 忽略绑定；出口按上方轮转策略轮换（未启用代理池时直连）',
  'Isolation on: identities egress only via their bound exits (main → backup); both down = skipped — never another exit or direct. Forced on when': '开启隔离：每个身份只从绑定的出口出站（主→备）；都不可用即跳过——绝不改走其他出口或直连。设有',
  '=true.': '=true 时强制开启。',
  'Enable backup proxies': '启用备用代理',
  'Off by default: main proxy only — cooling/removed = skipped for that round. On: backup acts as fallback.': '默认关闭：每个身份只走主代理——主代理冷却或被移除时该轮直接跳过。开启后：备用代理作为主代理的兜底。',
  // 网关设置页
  'API keys': 'API key',
  'Generated keys authenticate client access to the proxy API; they only take effect when the API_KEY environment variable is not set.': '生成的 key 用于客户端调用代理 API 鉴权，该 key 仅在未设置 API_KEY 环境变量时生效。',
  'Generate new key': '生成新 key',
  'Refresh models': '刷新模型',
  'Automatically synced from the official model source upstream (every 5 minutes)': '自动同步上游的官方模型源（每 5 分钟）',
  'Record admin logs': '记录管理日志',
  'Request headers (mimicking the Cline CLI)': '请求头（模拟 Cline CLI）',
  'Header': '请求头',
  'Value': '值',
  'Add header': '添加请求头',
  'Save headers': '保存请求头',
  'Attached to every Cline API request to mimic the official client.': '随每个转发到 Cline API 的请求附上，用于模拟官方客户端。',
  'Danger zone': '危险区',
  'Delete all data': '删除全部数据',
  'Deletes ALL persisted data (accounts, keys, proxies, config). Cannot be undone.': '删除所有持久化数据（所有凭据、代理、配置）。不可恢复。',
  'Delete ALL persisted data? This will clear Cline accounts and client keys, OpenCode keys, WorkBuddy accounts, the proxy pool, default model, request headers, scheduling strategy, custom aliases, sticky sessions, and request logs. This cannot be undone!': '确定删除全部持久化数据？将清空所有平台的凭据、代理池、默认模型、请求头、调度策略、自定义别名、粘性会话和请求日志，且不可撤销！',
  'no charge': '免费',
  // 请求日志页
  'last 500 entries, persisted to data/requests.jsonl': '最近 500 条，持久化到 data/requests.jsonl',
  'Auto-refreshing': '自动刷新中',
  'Pause': '暂停',
  'Time': '时间',
  'Source': '来源',
  'Method': '方法',
  'Path': '路径',
  'Model': '模型',
  'Route': '路由',
  'Upstream': '凭据',
  'Main': '主',
  'Backup': '备',
  'Direct': '直连',
  'Status': '状态',
  'Duration': '耗时',
  'cline': 'cline 池',
  'admin': '管理',
  'meta': '元数据',
  'other': '其他',
  // Combos 页
  'Create custom alias': '创建自定义别名',
  'Clients use the alias ID as the model name; the proxy rewrites it to the target. Alias must not collide with real model IDs, and the target must stay on the same platform.': '客户端以别名 ID 作为模型名，代理将其改写为目标模型。别名不得与真实模型 ID 冲突，且目标必须属于同一平台。',
  'Alias ID': '别名 ID',
  'Platform': '平台',
  'Existing custom aliases': '现有自定义别名',
  'Target model': '目标模型',
  // opencode 页
  'OpenCode': 'OpenCode',
  'On': '开',
  'Off': '关',
  'Probe model (used by the Test buttons):': '探测模型（Test 按钮使用）：',
  'Key': 'Key',
  'Session': '会话',
  'TTFB': '首包耗时',
  'Minted at': '铸造时间',
  'Rate-limit defense': '限流防御',
  // 提示气泡（title 属性）此前未进翻译表，中文界面下显示英文
  'Switch language / 切换语言': '切换语言',
  'cooling': '冷却中',
  'Base URL': 'Base URL',
  'Rate-limit defense': '限流防御',
  'Max concurrency': '最大并发',
  'Rate-limit retries': '限流重试次数',
  'Save rate-limit config': '保存限流配置',
  'Context compaction (OpenCode official mechanism)': '上下文压缩（OpenCode 官方机制）',
  'Auto-compaction': '自动压缩',
  'Reserved buffer': '预留缓冲',
  'Tail tokens kept': '尾部保留 token',
  'Summary model': '摘要模型',
  'empty = same as request model': '留空 = 与请求模型相同',
  'Summary cap': '摘要上限',
  'Save compaction config': '保存压缩配置',
  'Live session IDs (OpenCode FreeTier gate)': 'Live 会话 ID（OpenCode FreeTier 门槛）',
  'The gateway mints a CLI-format session ID per key on first use and keeps it sticky; IDs persist in the data volume and are refreshed locally after repeated 403s.': '网关在每个 key 首次使用时铸造一个 CLI 格式的会话 ID 并保持粘性；会话 ID 持久化在数据卷中，多次 403 后自动在本地换新。',
  '.': '。',
  'Context': '上下文',
  'Maximum context length': '最大上下文长度',
  'Today': '今日',
  'Total': '累计',
  'No data': '暂无数据',
  'total': '累计',
  'tokens (exact when upstream returns usage, otherwise estimated)': 'tokens（上游返回 usage 时精确，否则为估算）',
  // 模型状态标签
  'error': '错误',
  // 通用 toast / 确认 / 片段
  'Saved ': '已保存 ',
  'WorkBuddy accounts without proxy bindings follow this switch.': 'WorkBuddy 未绑定代理的账号遵循此开关。',
  'OpenRouter keys without proxy bindings follow this switch.': 'OpenRouter 未绑定代理的 key 遵循此开关。',
  'AMDCloud keys without proxy bindings follow this switch.': 'AMDCloud 未绑定代理的 key 遵循此开关。',
  'TokenHarbor keys without proxy bindings follow this switch.': 'TokenHarbor 未绑定代理的 key 遵循此开关。',
  'Other sub-platform keys without proxy bindings follow this switch.': 'Other 子平台未绑定代理的 key 遵循此开关。',
  'Pool': '启用',
  'Routing participation. New rows start disabled — tick Pool to enable.': '路由参与开关：新增行默认禁用，勾选 Pool 后参与轮转。',
  'Routing participation: unchecked keys never enter request rotation. Newly added keys start disabled — tick Pool to enable. Minting follows routing — keys with routing disabled stop auto-minting.': '路由参与开关：不勾选的 key 不进入请求轮转，铸造跟随路由（路由禁用的 key 不再自动铸造）。新增 key 默认禁用，勾选 Pool 后启用。',
  'Save failed: ': '保存失败：',
  'Test failed: ': '测试失败：',
  ' — cooling': ' —— 冷却中',
  'Check failed: ': '检查失败：',
  'Delete failed: ': '删除失败：',
  'Refresh failed: ': '刷新失败：',
  ' started': ' 已开始',
  ' failed: ': ' 失败：',
  ' completed': ' 已完成',
  'Requesting...': '请求中...',
  'Admin session expired': '管理员会话已过期',
  'Bind main proxy by row order: 1st account to Direct, then the proxy pool in order (staged - click Save to apply; enable/disable and backup untouched)': '按行序绑定主代理：第 1 个账号绑 Direct，其余依代理池顺序（暂存，点「保存」生效；不影响启用/禁用与备用绑定）',
  'Re-sync the model catalog from the Trae upstream using an enabled account': '使用已启用账号从 Trae 上游重新同步模型目录',
  'Export failed: ': '导出失败：',
  'Update failed: ': '更新失败：',
  'Generation failed: ': '生成失败：',
  'Add failed: ': '添加失败：',
  'Import failed: ': '导入失败：',
  'Create failed: ': '创建失败：',
  'Failed to load accounts: ': '账号加载失败：',
  'Failed to load: ': '加载失败：',
  'All persisted data deleted': '已删除全部持久化数据',
  'Account added: ': '账号已添加：',
  'Account ': '账号 ',
  ' — ': ' —— ',
  'Available': '可用',
  'Error': '错误',
  '\nEstimated recovery: ': '\n预计恢复：',
  '\nReason: ': '\n原因：',
  '\nHTTP: ': '\nHTTP：',
  ' (estimated recovery ': '（预计恢复 ',
  ' (remaining ': '（剩余 ',
  ')': '）',
  'OK': '成功',
  ' — cooldown cleared': ' —— 冷却已清除',
  ' via ': '，模型 ',
  'Combo created: ': '自定义别名已创建：',
  'Delete combo ': '删除自定义别名 ',
  '? Clients using this alias will no longer be able to call it.': '？使用该别名的客户端将无法继续调用。',
  'Delete this account?': '确定删除该账号？',
  'Delete this key?': '确定删除该 key？',
  'OAuth failed: ': 'OAuth 登录失败：',
  'Recovers ': '恢复于 ',
  // ===== OpenRouter / AMDCloud / TokenHarbor 页（目录/限流/弹窗） =====
  'Model catalog': '模型目录',
  'Upstream limits: 30 requests/min per key, 8 concurrent per key, 20 requests/min per account.': '上游限制：单个密钥 30 次/分钟、8 并发，单个账号 20 次/分钟。',
  'Upstream limits: 60 requests/min per account, 100 requests/min per IP.': '上游限制：每账号 60 次/分钟，每 IP 100 次/分钟。',
  'Re-sync the free-model catalog from openrouter.ai': '从 openrouter.ai 重新同步免费模型目录',
  'Re-sync the model catalog from AMDCloud': '从 AMDCloud 重新同步模型目录',
  'Re-sync the free-model catalog from TokenHarbor (needs a key)': '从 TokenHarbor 重新同步免费模型目录（需要 key）',
  'Add OpenRouter keys': '添加 OpenRouter key',
  'One OpenRouter API key per line (sk-or-v1-…). New keys start with routing disabled — tick Pool to enable.': '每行一个 OpenRouter API key（sk-or-v1-…）。新 key 默认不参与路由——勾选"启用"后生效。',
  'Add AMDCloud keys': '添加 AMDCloud key',
  'One AMDCloud API key per line (rc-…, issued at the': '每行一个 AMDCloud API key（rc-…，获取自',
  '). New keys start with routing disabled — tick Pool to enable.': '）。新 key 默认不参与路由——勾选"启用"后生效。',
  'Add TokenHarbor keys': '添加 TokenHarbor key',
  'One TokenHarbor universal key per line (thk_live_…, from the': '每行一个 TokenHarbor 通用 key（thk_live_…，获取自',
  'dashboard': '控制台',
  'OpenRouter config saved': 'OpenRouter 配置已保存',
  'AMDCloud config saved': 'AMDCloud 配置已保存',
  'TokenHarbor config saved': 'TokenHarbor 配置已保存',
  'Catalog refreshed': '目录已刷新',
  'No free models yet — click Refresh models': '暂无免费模型——点"刷新模型"',
  'No models yet — click Refresh models': '暂无模型——点"刷新模型"',
  ' free models': ' 个免费模型',
  'Failed to load': '加载失败',
  'Invalid proxy format: ': '代理格式无效：',
  'No models': '没有模型',
  '· official feed: ': '· 官方源同步于 ',
  'Check complete': '检查完成',
  'cooldown': '冷却中',
  'active': '可用',
  'expired': '已失效',
  // Other 页（多子平台）
  'Other': 'Other',
  'Sub-platform': '子平台',
  'New sub-platform': '新建子平台',
  'Manage sub-platforms': '管理子平台',
  'Reorder or delete sub-platforms': '排序或删除子平台',
  'Manage': '管理',
  'Drag a row to change the order; the order decides how sub-platforms appear in the selector.': '拖动行可调整顺序；该顺序决定子平台在下拉框中的显示顺序。',
  'Save order': '保存顺序',
  'Drag to reorder': '拖动以排序',
  'Delete this key': '删除此密钥',
  'Name': '名称',
  'Name *': '名称 *',
  'Models': '模型',
  'Model prefix (required)': '模型前缀（必填）',
  'Model prefix (required, unique) *': '模型前缀（必填，唯一）*',
  'Name and prefix are required': '名称和前缀为必填项',
  'No sub-platform yet. Click "New sub-platform" to add one.': '暂无子平台。点击"新建子平台"添加一个。',
  'Select a sub-platform to view details': '请选择子平台以查看详情',
  'No sub-platform yet — click "New sub-platform" to add one': '还没有子平台 —— 点击"新建子平台"添加',
  'Select a sub-platform on the left.': '在左侧选择一个子平台。',
  ' sub-platform(s). Models are exposed as <prefix>:<model-id>': ' 个子平台。模型以 <prefix>:<model-id> 形式对外暴露',
  'Add a credential, then refresh models or add one manually': '请添加凭据后刷新模型或手动添加模型',
  'Add a credential first': '请先添加凭据',
  'No available models': '暂无可用模型',
  'No available credentials': '暂无可用凭据',
  'Real request': '真实请求',
  'Re-sync the model catalog from the upstream (needs a credential)': '从上游重新同步模型目录（需要凭据）',
  'Send a real chat request using the first enabled credential': '使用第一个已启用凭据发送真实会话请求',
  'Sending…': '发送中…',
  'Request succeeded': '请求成功',
  'Request failed': '请求失败',
  'Test message, please reply with hello only': '测试消息，请只回复你好',
  'Syncing models…': '正在同步模型…',
  'Model catalog synced': '模型目录已同步',
  'Add model': '添加模型',
  'Remove this manually added model': '删除这个手动添加的模型',
  'Manual model removed': '已删除手动添加的模型',
  'Remove failed: ': '删除失败：',
  'add model id manually': '手动输入模型 id',
  'Enter a model id': '请输入模型 id',
  'No models yet — add manually or add a key and click Refresh models': '暂无模型 —— 可手动添加，或添加密钥后点击"刷新模型"',
  ' enabled': ' 已启用',
  'none enabled': '未启用',
  'Models updated': '模型已更新',
  'Create': '创建',
  'Sub-platform created': '子平台已创建',
  'Added': '已添加',
  ' keys': ' 个密钥',
  'Test passed': '测试通过',
  'Saved': '已保存',
  'No keys configured': '未配置密钥',
  'Create an entry for an upstream provider: shared Base URL, its API keys, and the model prefix its models are exposed under.': '为上游厂商创建条目：共享的 Base URL、其 API 密钥，以及模型对外暴露的前缀。',
  'One API key per line for this sub-platform. New keys start with routing disabled — tick Pool to enable.': '每行一个 API 密钥。新密钥默认不参与路由 —— 勾选 Pool 启用。',
  'No accounts yet': '暂无账号信息,请添加',
  'Ready': '就绪',
  'Authenticated': '已认证',
  'Cooling': '冷却中',
  'Suspended': '已暂停',
  'Unknown': '未知',
  'Balances refreshed': '余额已刷新',
  'Requesting authorization link...': '正在请求授权链接...',
  'Open the link and complete the browser login.': '打开链接并完成浏览器登录。',
  'Authorization session created.': '授权会话已创建。',
  'WorkBuddy account added': 'WorkBuddy 账号已添加',
  'Authorization link copied': '授权链接已复制',
  'Test whether the account works (success clears cooldown/expired state)': '测试账号是否可用（成功会解除冷却/过期状态）',
  'Active': '活跃',
  'Expired': '已失效',
  'No proxies configured': '未配置代理',
  'OpenCode config saved': 'OpenCode 配置已保存',
  'Follows the main slot': '跟随主出口',
  'PROXY_ISOLATION=true is set — isolation is forced on. Unset the env var (or set it to false) to control isolation from the panel.': '已设置 PROXY_ISOLATION=true —— 隔离被强制开启。删除该环境变量（或设为 false）才能在面板控制隔离。',
  'Backup proxies are disabled — follows the main slot': '备用代理已关闭 —— 跟随主代理',
  'Admin access logging enabled': '已开启管理页访问日志',
  'Admin access logging disabled': '已关闭管理页访问日志',
  'Paused': '已暂停',
  'Resume': '继续',
  'No requests logged': '暂无请求记录',
  'No combos yet — create one with the form above.': '还没有自定义别名 —— 用上方表单创建一个。',
  'Failed to load model list': '模型列表加载失败',
  'socks5 pool': 'socks5 池',
  'stale': '已失效',
  'Pending activation': '待启用',
  'Subscription URL': '订阅链接（优先）',
  'Managed by the subscription URL — clear it to edit manually': '由订阅链接管理——清空后才能手动编辑',
  'http://10.10.10.5:12321/proxy/socks.txt — the pool is then managed by this subscription; leave empty to edit the list manually': 'http://10.10.10.5:12321/proxy/socks.txt —— 填写后代理池由订阅管理；留空则手动编辑列表',
  'Update frequency (cron)': '更新频率',
  'Update now': '立即更新',
  'Updating...': '更新中...',
  'Update done': '更新完成',
  'Subscription refresh replaces the whole pool with the fetched list. Fetch failures keep the current pool.': '订阅刷新会用拉取到的列表整池替换，拉取失败保留现池不变。',
  '*/30 * * * * (min hour day month weekday)': '*/30 * * * *（分 时 日 月 周）',
  'no key': '无 key',
  'Click to copy': '点击复制',
  'Remove this key from the pool': '将该密钥移出池',
  'next in rotation': '轮换顺序中的下一个',
  'Proxy pool saved': '代理池已保存',
  'Global policy saved': '全局策略已保存',
  'Proxy isolation settings saved': '代理隔离设置已保存',
  'Account deleted': '账号已删除',
  'Key deleted': 'key 已删除',
  'Key generated': 'key 已生成',
  'New key generated (click to copy)': '新 key 已生成（点击复制）',
  'Copied to clipboard': '已复制到剪贴板',
  'Rows with a value but no key were ignored': '有值但没有键名的行已被忽略',
  'Headers saved': '请求头已保存',
  'Enter a token or API key': '请输入 token 或 API key',
  'Enter an alias ID': '请输入别名 ID',
  'Select a target model': '请选择目标模型',
  'Combo deleted': '自定义别名已删除',
  'Testing': '测试中',
  'Checking': '检查中',
  'No opencode keys configured': '没有配置任何 opencode key',
  'No OpenRouter keys configured': '没有配置任何 OpenRouter key',
  'No AMDCloud keys configured': '没有配置任何 AMDCloud key',
  'No TokenHarbor keys configured': '没有配置任何 TokenHarbor key',
  'Probe the rate limit; if still limited, stays cooling with a recovery time': '探测限流状态；若仍在限流则保持冷却并显示恢复时间',
  'Batch test done': '批量测试完成',
  // 静态 hint 段落与状态标签补漏（TreeWalker 按文本节点整段匹配）
  'Counted since process start.': '统计自进程启动起。',
  'No API keys yet': '还没有 API key',
  '— no sub-platform yet —': '— 还没有子平台 —',
  'Search models…': '搜索模型…',
  'Average tunnel-setup time over successful attempts (TCP+TLS+CONNECT handshake only — excludes upstream first byte and task duration). Lower is better.': '成功请求的平均隧道建立耗时（仅统计 TCP+TLS+CONNECT 握手，不含上游首包与任务耗时）。越低越好。',
  'live': '活跃',
  'n/a': '无',
  'not minted': '未铸造',
  // 封禁模型区（模型黑名单）
  'Banned models': '封禁模型',
  'Ban reason': '封禁原因',
  'Ban': '封禁',
  'Blocked at': '封禁时间',
  'Unblock': '解禁',
  'No blocked models': '暂无被封禁的模型',
  'Testing...': '测试中…',
  'reachable — you can unblock it': '请求成功——可以解禁',
  'Unblock this model? It becomes available in /v1/models again; the same upstream error will ban it again.': '解禁该模型？它会重新出现在 /v1/models；若上游仍报同样的错误，程序会再次封禁。',
  'Ban this model? It is removed from /v1/models immediately; you can unblock it in the Banned models table.': '封禁该模型？它会立即从 /v1/models 移除；之后可在封禁模型表中解禁。',
  'Banned: ': '已封禁：',
  'Unblocked: ': '已解禁：',
  // Trae 平台页
  'Gateway': '网关',
  'Endpoint': '端点',
  'Auto route': '自动路由',
  'Upstream endpoint switched': '上游端点已切换',
  'Upstream endpoint: Auto = merged catalog via the native raw_chat transport; Remote / TraeWork switch requests to the chat_sessions transport (same as the upstream project)': '上游端点：自动 = 合并目录走原生 raw_chat 传输；Remote / TraeWork 切换为 chat_sessions 传输（与原项目一致）',
  'Counted locally by this proxy from upstream usage events': '由本网关按上游用量事件本地计数',
  'Re-sync the model catalog from the TraeWork upstream using an enabled account': '用一个已启用的账号从 TraeWork 上游重新同步模型目录',
  'Models appear here once an account is enabled — the catalog syncs from upstream': '启用账号后此处才会显示并同步模型目录',
  'Models appear here once a credential is enabled — the catalog syncs from upstream': '启用账号或 key 后此处才会显示并同步模型目录',
  'no enabled AMDCloud key — enable a key first, then refresh': '没有启用的 AMDCloud key —— 请先启用 key 再刷新',
  'no enabled Trae account — enable an account first, then refresh': '没有启用的 TraeWork 账号 —— 请先启用账号再刷新',
  'no enabled OpenCode key — enable a key first, then refresh': '没有启用的 OpenCode key —— 请先启用 key 再刷新',
  'no enabled OpenRouter key — enable a key first, then refresh': '没有启用的 OpenRouter key —— 请先启用 key 再刷新',
  'no enabled key on this platform — enable a key first, then refresh': '该平台没有启用的 key —— 请先启用 key 再刷新',
  'no enabled Cline account — enable an account first, then refresh': '没有启用的 Cline 账号 —— 请先启用账号再刷新',
  'no enabled TokenHarbor key — enable a key first, then refresh': '没有启用的 TokenHarbor key —— 请先启用 key 再刷新',
  'model sync started': '模型同步已启动',
  'synced, ': '已同步 ',
  ' new models': ' 个新模型',
  'Sync started': '同步已启动',
  'Add accounts': '添加账号',
  'No TraeWork accounts yet — click Add accounts': '暂无 TraeWork 账号——点"添加账号"',
  'Add TraeWork accounts': '添加 TraeWork 账号',
  'Tokens (one per line)': 'Token（每行一个）',
  'Automation': '自动化',
  'Token valid': 'token 有效',
  'Token expired': 'token 已过期',
  'No refresh token': '无刷新令牌',
  'Unlimited': '不限量',
  ' models': ' 个模型',
  'Account #': '账号 #',
  ' — ': ' — ',
  ' — unlimited credits': ' — 不限量额度',
  ' — credits: ': ' — 额度：',
  'No accounts to test': '没有可测试的账号',
  'Enter at least one token': '请至少输入一个 token',
  ', skipped ': '，跳过 ',
  'Remove this account from the pool': '把该账号移出账号池',
  'TraeWork config saved': 'TraeWork 配置已保存',
  ' — re-download it from THIS panel and restart it (python web_login.py)': ' ——请从本面板重新下载助手并重启（python web_login.py）',
  'Helper detected, but it writes credentials to ANOTHER deployment: ': '检测到助手，但它会把凭据写入另一个部署：',
  'Local helper detected (writes to this gateway) — click Open helper page, then use the authorization button there': '已检测到本机授权助手（凭据写入本网关）——点"打开授权助手页面"，在弹出的页面里点授权按钮',
  'Download helper': '下载授权助手',
  'Download the helper from this panel, run "python web_login.py" locally, then authorize on the opened page (sign in to trae.cn in the browser first). The account lands here automatically with routing off. The helper writes to the panel it was downloaded from.': '从本面板下载助手并在本机运行 python web_login.py，在打开的页面点授权即可（浏览器需已登录 trae.cn）。账号自动入库，默认不参与路由；助手写入下载它的那个网关。',
  'One Cloud-IDE-JWT (a 3-part JWT) per line. New accounts start with routing off — tick the Gateway switch to enable.': '每行一个 Cloud-IDE-JWT（三段式）。新账号默认不参与路由，勾选反代开关启用。',
  'Open the helper page (127.0.0.1:8765) and click the authorize button there': '打开助手页面（127.0.0.1:8765），在页面里点授权按钮',
  'Web authorization': '网页授权',
  'Manual paste': '手动填写',
  'Discard staged selections and reload the saved ban state': '放弃暂存的勾选，恢复为已保存的封禁状态',
  'Staged selections reset': '已复位暂存的勾选',
  'Open helper page': '打开授权助手页面',
  'Checking local helper…': '正在检测本机授权助手…',
  'Local helper NOT detected — download it, run "python web_login.py", then retry': '未检测到本机授权助手——先下载并在本机运行 web_login.py 后重试',
  // Trae 双开关 / 网页授权 / 签到 / 自动化
  'Refresh balances': '刷新余额',
  'Refresh every enabled account\'s credit snapshot (feeds the Credits column and credit-priority selection)': '刷新所有已启用账号的额度快照（额度列与额度优先选号的数据源）',
  'Daily check-in for every enabled account that has not checked in today': '给今天还没签到的已启用账号执行每日签到',
  'Daily check-in': '每日自动签到',
  'Checked in': '签到成功',
  'Check-in failed': '签到失败',
  'Check-in done: ': '签到完成：',
  ' already': ' 个已签过',
  ' failed': ' 个失败',
  'Account ready: ': '账号就绪：',
  'Account already in the pool: ': '账号已在池中：',
  'Authorization failed: ': '授权失败：',
  'Selection strategy': '选号策略',
  'Round-robin': '轮转',
  'Credit priority (richest first)': '额度优先（余量多者优先）',
  'Credit priority picks the account with the most remaining credits first (unlimited accounts always first; needs the background balance refresh). Daily check-in claims every enabled account once per day.': '额度优先：余量最多的账号优先（不限量账号恒最优先；依赖后台余额刷新的数据）。每日签到会给每个已启用账号每天签一次。',
  'Save automation config': '保存自动化配置',
  'Remaining credits from the latest snapshot (auto-refreshed in the background; Test refreshes on demand)': '最新快照的剩余额度（后台自动刷新；Test 按钮即时刷新）',
  'Account master switch. Off = the account does not exist: check-in, balance refresh and Test all skip it, and it never routes.': '账号总开关。关闭 = 账号视为不存在：签到、余额刷新与 Test 全部跳过，也绝不参与路由。',
};
function T(s) { return LANG === 'zh' ? (I18N_ZH[s] || s) : s; }
function toggleLang() {
  localStorage.setItem('lang', LANG === 'zh' ? 'en' : 'zh');
  location.reload();
}
// 文本节点翻译：保留首尾空白（HTML 缩进），整段替换
function i18nTextNode(n) {
  const raw = n.nodeValue;
  const s = raw.trim();
  if (!s) return;
  const t = I18N_ZH[s];
  if (!t) return;
  const lead = raw.slice(0, raw.length - raw.trimStart().length);
  const trail = raw.slice(raw.trimEnd().length);
  n.nodeValue = lead + t + trail;
}
function i18nAttrs(el) {
  if (!el.getAttribute) return;
  ['title', 'placeholder'].forEach(a => {
    const v = el.getAttribute(a);
    if (!v) return;
    const t = I18N_ZH[v.trim()];
    // Same-value setAttribute still emits an attribute mutation and can loop
    // forever when a translated placeholder/title maps to itself.
    if (t && t !== v) el.setAttribute(a, t);
  });
}
function i18nSubtree(root) {
  if (LANG !== 'zh') return;
  const w = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  while (w.nextNode()) i18nTextNode(w.currentNode);
  if (root.querySelectorAll) {
    root.querySelectorAll('[title],[placeholder]').forEach(i18nAttrs);
  }
  if (root.nodeType === 1) i18nAttrs(root);
}
// 观察动态插入（表格行/toast/状态标签等）：纯英文文本节点即时翻译。
// 翻译写入自身触发的新 mutation 会查中文键必 miss，自动收敛不会死循环。
const i18nMO = new MutationObserver(muts => {
  if (LANG !== 'zh') return;
  for (const m of muts) {
    if (m.type === 'attributes') { i18nAttrs(m.target); continue; }
    for (const n of m.addedNodes) {
      if (n.nodeType === 3) i18nTextNode(n);
      else if (n.nodeType === 1) i18nSubtree(n);
    }
  }
});
i18nMO.observe(document.body, { childList: true, subtree: true, attributes: true, attributeFilter: ['title', 'placeholder'] });
i18nSubtree(document.body);
if (LANG === 'zh') document.documentElement.lang = 'zh-CN';
(function initLangBtn() {
  const b = _('langBtn');
  if (b) b.textContent = LANG === 'zh' ? 'EN' : '中';
})();

if (window.location.host) {
  _('footerApiAddr').textContent = 'http://' + window.location.host;
}

function toast(msg, t, duration) {
  const el = _('toast');
  el.textContent = msg;
  el.style.whiteSpace = 'pre-line';
  el.className = 'toast ' + (t || 'info') + ' show';
  clearTimeout(el._hideTimer);
  try {
    if (el.matches(':popover-open')) el.hidePopover();
    el.showPopover();
  } catch (e) { /* older browsers fall back to z-index stacking */ }
  clearTimeout(el._timer);
  el._timer = setTimeout(() => {
    el.classList.remove('show');
    el._hideTimer = setTimeout(() => {
      try { if (el.matches(':popover-open')) el.hidePopover(); } catch (e) { /* ignore */ }
    }, 260);
  }, duration || 3500);
}
function showWorkbuddyDialog(dialog) {
  if (dialog && !dialog.open) dialog.showModal();
  // 重新打开通知可把它重新推到 top layer 顶部，避免先弹通知、后开弹窗时被遮住。
  const el = _('toast');
  try {
    if (el && el.matches(':popover-open')) {
      el.hidePopover();
      el.showPopover();
    }
  } catch (e) { /* ignore */ }
}

// ========== Navigation ==========
function ensureWorkbuddyConsole() {
  loadWorkbuddyAccounts();
  loadWorkbuddyConfig();
}
document.querySelectorAll('.nav-item').forEach(el => {
  el.addEventListener('click', () => {
    if (el.classList.contains('active')) return;
    switchTab(el.dataset.tab);
  });
});
// restoreTab 刷新/重开后恢复上次浏览的页：switchTab 把当前页写进 location.hash，
// 这里只在 hash 命中真实存在的 tab 时才恢复（tab-* 元素存在性即白名单）。
// 注意只能声明不能立即调用：switchTab 会触达各平台的 const（如 WB_API_PREFIX），
// 在脚本中段执行会撞 TDZ（"Cannot access before initialization"），调用点在末尾 Init 区。
function restoreTab() {
  const h = (location.hash || '').replace(/^#/, '');
  if (h && _('tab-' + h)) switchTab(h);
}

function switchTab(name) {
  document.querySelectorAll('.nav-item').forEach(e => {
    e.classList.toggle('active', e.dataset.tab === name);
  });
  document.querySelectorAll('.tab-panel').forEach(e => e.style.display = 'none');
  _('tab-' + name).style.display = 'block';
  if (name === 'dashboard') { loadStats(); loadAccounts(); }
  if (name === 'accounts') { loadAccounts(); loadModels(); }
  if (name === 'trae') loadTraeConfig();
  if (name === 'settings') { loadKeys(); loadConfig(); loadPlatformBans(); }
  if (name === 'logs') loadLogs();
  if (name === 'proxypool') loadProxyPool();
  if (name === 'opencode') { loadOcConfig(); loadOcModels(); loadOcSessions(); loadModels(); }
  if (name === 'workbuddy') { ensureWorkbuddyConsole(); loadModels(); }
  if (name === 'openrouter') loadOrConfig();
  if (name === 'amdcloud') loadAmdConfig();
  if (name === 'tokenharbor') loadTHConfig();
  if (name === 'other') loadOtherConfig();
  if (name === 'combos') { loadCombos(); fillComboModels(); }
  // 把当前页写进 hash：刷新/重开浏览器后 restoreTab 恢复到离开时的页
  try { history.replaceState(null, '', '#' + name); } catch (e) { /* ignore */ }
}

// ========== Integrated WorkBuddy console ==========
const WB_API_PREFIX = '/admin/workbuddy/api';
let wbAccounts = [];
let wbProxyCache = { bindings: {} };
let wbOAuthTimer = null;

async function wbCall(action, params, body) {
  const query = new URLSearchParams();
  Object.entries(params || {}).forEach(([k, v]) => {
    if (v !== undefined && v !== null && v !== '') query.set(k, String(v));
  });
  const init = { method: body ? 'POST' : 'GET', headers: { 'Accept': 'application/json' } };
  if (body) {
    init.headers['Content-Type'] = 'application/json';
    init.body = JSON.stringify(body);
  }
  const qs = query.toString();
  const encodedAction = action.split('/').map(encodeURIComponent).join('/');
  const res = await fetch(WB_API_PREFIX + '/' + encodedAction + (qs ? '?' + qs : ''), init);
  if (res.status === 401) {
    location.reload();
    throw new Error(T('Admin session expired'));
  }
  let data = {};
  try { data = await res.json(); } catch (e) { /* preserve HTTP error below */ }
  if (!res.ok) throw new Error(data.error || data.message || ('HTTP ' + res.status));
  if (data.ok === false || data.success === false) throw new Error(data.error || data.message || ('HTTP ' + res.status));
  if (data.error && !data.config && !data.accounts) throw new Error(data.error);
  return data.data !== undefined ? data.data : data;
}

function wbDig(obj, path) {
  return String(path || '').split('.').reduce((v, k) => (v == null ? undefined : v[k]), obj);
}
function wbPut(obj, path, value) {
  const keys = String(path || '').split('.').filter(Boolean);
  if (!keys.length) return;
  let cur = obj;
  keys.slice(0, -1).forEach(k => {
    if (!cur[k] || typeof cur[k] !== 'object') cur[k] = {};
    cur = cur[k];
  });
  cur[keys[keys.length - 1]] = value;
}
function wbRowId(account) {
  const value = account && (account.uid ?? account.account_id ?? account.id);
  return value === undefined || value === null ? '' : String(value);
}
function wbStatusInfo(account) {
  const status = String(account.status || (account.disabled ? 'disabled' : (account.cooling ? 'cooling' : 'active'))).toLowerCase();
  if (status === 'ready' || status === 'authenticated' || status === 'active' || status === 'enabled') {
    return { cls: 'active', label: status === 'authenticated' ? 'Authenticated' : (status === 'active' ? 'Active' : 'Ready') };
  }
  if (status === 'cooling' || status === 'cooldown' || status === 'suspended') {
    return { cls: 'cooldown', label: status === 'suspended' ? 'Suspended' : 'Cooling' };
  }
  if (status === 'disabled' || status === 'deleted' || status === 'error' || status === 'failed') {
    return { cls: 'expired', label: status === 'deleted' ? 'Deleted' : (status === 'disabled' ? 'Disabled' : 'Error') };
  }
  return { cls: 'expired', label: status === 'unknown' ? 'Unknown' : status };
}
function wbFmtTokens(value) {
  const n = Number(value || 0);
  if (n >= 1000000) return (n / 1000000).toFixed(2).replace(/\.?0+$/, '') + 'M';
  if (n >= 1000) return (n / 1000).toFixed(1).replace(/\.0$/, '') + 'K';
  return String(n);
}
function wbBindingCell(account) {
  const id = wbRowId(account);
  const binding = (wbProxyCache.bindings || {})[id] || {};
  const main = String(binding.main ?? binding.main_proxy_id ?? '');
  const backup = String(binding.backup ?? binding.backup_proxy_id ?? '');
  // 备用代理开关关闭 → 备用槽只读跟随主槽（同主=全局/直连 时的联动）
  const backupLocked = !backupProxyOn || main === '' || main === EGRESS_DIRECT;
  const backupVal = backupLocked ? main : backup;
  // 快照记渲染后的生效值（备用槽锁定时 = 主槽），保存 diff 与之同口径。
  // 若拿原始绑定值对比，锁定显示本身就会被误判成用户修改——备用代理开关
  // 关闭时每行都中（保存提示 N 个账号全改过，实际只改了一项）。
  if (wbSnapshot[id]) { wbSnapshot[id].main = main; wbSnapshot[id].backup = backupVal; }
  const stale = proxyStale;
  const staleMark = (stale(main) || stale(backup))
    ? ' <span title="' + T('Bound proxy is no longer in the proxy pool — skipped until fixed') + '" style="color:var(--danger)">⚠</span>'
    : '';
  return '<td style="white-space:nowrap">' +
    egComboHTML('wb', id, 'main', main, T('Main egress: requests go through this egress whenever available')) + ' ' +
    egComboHTML('wb', id, 'backup', backupVal, T('Backup egress: fallback while the main egress is cooling down or unavailable'), backupLocked) +
    staleMark +
    '</td>';
}
function wbActionButton(action, id, label, extra) {
  return '<button type="button" class="btn btn-sm ' + (extra || '') + '" data-wb-action="' + esc(action) + '" data-wb-id="' + esc(id) + '" onclick="workbuddyActionFrom(this)">' + esc(label) + '</button>';
}
function renderWorkbuddyAccounts() {
  const tbody = _('wbAccountsBody');
  if (!tbody) return;
  if (!wbAccounts.length) {
    tbody.innerHTML = '<tr><td colspan="10" class="empty">No accounts yet</td></tr>';
    return;
  }
  wbSnapshot = {};
  tbody.innerHTML = wbAccounts.map((account, i) => {
    const id = wbRowId(account);
    const status = wbStatusInfo(account);
    const realm = String(account.realm || 'cn');
    const usage = account.token_usage || {};
    const tokensToday = usage.today_tokens ?? 0;
    const tokensTotal = usage.total_tokens ?? 0;
    const lastUsed = usage.last_used_at || account.last_success;
    const credits = account.credits ?? account.remaining_credits ?? '—';
    const creditsTotal = account.credits_total ?? account.total_credits;
    const reason = account.disabled_reason || account.reason || '';
    // 状态单元格结构与 cline 页完全一致：chip + （冷却中的）恢复时刻小字，
    // 不再常驻渲染 reason 第三行；原因收进 chip 的 title 悬浮（诊断信息不丢）。
    const statusHtml = '<span class="status ' + status.cls + '"' + (reason ? ' title="' + esc(status.label + ' · ' + reason) + '"' : '') + '><span class="status-dot ' + status.cls + '"></span>' + esc(status.label) + '</span>' +
      // 冷却中补恢复时刻（与 cline 页同款小字；完整读数走 title）
      (status.cls === 'cooldown' && account.until && !String(account.until).startsWith('0001-')
        ? '<div style="font-size:10px;color:var(--text3);margin-top:2px;white-space:nowrap" title="' + esc(fmtWhen(account.until)) + '">' + T('Recovers ') + esc(fmtShort(account.until)) + '</div>'
        : '');
    // 竖排签到徽章（2026-10-27，与 TraeWork 页统一）：尝试日=今天且未成功 →
    // 签到失败（title=原因）；今天已签 → 签到成功。global 域无签到体系不显示。
    const wbToday = new Date().toLocaleDateString('sv');
    const wbAttemptDay = account.last_checkin_attempt_day || '';
    const wbCheckinOk = account.checkin_done === true;
    let checkinV = '';
    if (realm !== 'global' && (wbCheckinOk || wbAttemptDay === wbToday)) {
      const ok = wbCheckinOk;
      const txt = ok ? T('Checked in') : T('Check-in failed');
      const cls = ok ? 'status active' : 'status cooldown';
      checkinV = '<div class="' + cls + '" style="writing-mode:vertical-rl;text-orientation:upright;letter-spacing:2px;font-size:10px;padding:5px 2px;border-radius:8px;align-self:stretch;display:flex;align-items:center;justify-content:center" title="' + esc(account.last_checkin_error || txt) + '">' + esc(txt) + '</div>';
    }
    // 账号/反代双开关列（afree）：账号开关=总开关（关 = 账号不存在——签到/任务/
    // 余额刷新全停，且不再路由）；反代开关=只管会话路由（关 = 只是不接会话，
    // 签到/保活等任务照常）。互锁：反代开关只有账号开关开启时可勾（后端 API
    // 同样校验）。勾选改动先暂存，由"保存"按钮统一提交。
    const accEnabled = account.account_enabled !== false;
    const poolOn = account.pool_enabled !== false;
    wbSnapshot[id] = { account: accEnabled, enabled: poolOn };
    const accCell = '<td style="text-align:center"><input type="checkbox" data-wb-account="' + esc(id) + '"' +
      (accEnabled ? ' checked' : '') + ' title="' + esc(T('Account master switch: off = the account does not exist — check-in, tasks and balance refresh all stop, and it never routes')) + '" aria-label="' + esc(T('Account switch')) + '"></td>';
    const poolCell = '<td style="text-align:center"><input type="checkbox" data-wb-pool="' + esc(id) + '"' +
      (accEnabled && poolOn ? ' checked' : '') + (!accEnabled ? ' disabled' : '') +
      ' title="' + esc(T('Gateway switch: unchecked = model sessions never route to this account; check-in and all other maintenance tasks are unaffected')) + '" aria-label="' + esc(T('Gateway switch')) + '"></td>';
    // uid 过长会把 Account 列撑宽，昵称下方只留前 10 个字符，完整值走 title 悬浮。
    const shortId = id.length > 10 ? id.slice(0, 10) + '…' : id;
    // 账号列：昵称旁铅笔行内改名。手改后加本地锁（上游昵称同步跳过，不被
    // 手动全量刷新覆盖）；清空 = 解锁并从上游拉当前昵称恢复。铅笔回显当前
    // **显示值**；可编辑值提交后由后端落 nickname 字段。
    const wbNick = account.nickname || '';
    const wbDisplayName = account.name || account.email || wbNick || ('Account ' + id);
    return '<tr draggable="true" data-wb-id="' + esc(id) + '" data-drag-id="' + esc(id) + '">' +
      '<td class="mono" style="font-size:11px;color:var(--text3)">#' + (i + 1) + '</td>' +
      '<td><div class="wb-account"><div class="wb-account-main">' +
        '<div class="nick-wrap">' +
          '<span class="wb-account-name nick-name" title="' + esc(wbDisplayName) + '">' + esc(wbDisplayName) + '</span>' +
          nickPen(id, wbDisplayName, T('Edit nickname')) +
        '</div>' +
        '<div class="wb-account-id" title="' + esc(realm + ' · ' + id) + '">' + esc(realm) + ' · ' + esc(shortId) + '</div>' +
      '</div></div></td>' +
      '<td><div style="display:flex;gap:6px;align-items:center">' +
        '<div style="display:flex;flex-direction:column;gap:2px;min-width:0">' + statusHtml + '</div>' + checkinV +
      '</div></td>' +
      '<td>' + esc(credits) + (creditsTotal ? '<span style="color:var(--text3)"> / ' + esc(creditsTotal) + '</span>' : '') + '</td>' +
      '<td title="' + T('Today') + ' ' + fmtNum(tokensToday) + ' / ' + T('total') + ' ' + fmtNum(tokensTotal) + ' ' + T('tokens (exact when upstream returns usage, otherwise estimated)') + '">' + fmtTokens(tokensToday) + ' / ' + fmtTokens(tokensTotal) + '</td>' +
      '<td class="mono" style="font-size:11px">' + (lastUsed ? esc(fmtLastUsed(lastUsed)) : '-') + '</td>' +
      wbBindingCell(account) +
      '<td style="white-space:nowrap;text-align:center"><div class="wb-action-group">' +
        wbActionButton('test', id, 'Test') +
        wbActionButton('details', id, 'Points') +
        wbActionButton('delete', id, 'Delete', 'btn-danger') +
      '</div></td>' +
      poolCell +
      accCell +
      '</tr>';
  }).join('');
  tbody.onchange = e => {
    // 账号开关切换：联动反代开关的可用态——关闭账号时反代勾选框禁用并显示
    // 未勾选；重新开启时恢复其持久化值（存储未动，重开即恢复路由意愿）。
    const acc = e.target.closest('input[data-wb-account]');
    if (acc) {
      const row = acc.closest('tr');
      const poolCb = row.querySelector('input[data-wb-pool]');
      if (poolCb) {
        poolCb.disabled = !acc.checked;
        if (!acc.checked) poolCb.checked = false;
        else {
          const snap = wbSnapshot[poolCb.dataset.wbPool];
          poolCb.checked = snap ? snap.enabled !== false : true;
        }
      }
      markRowDirty(row, 'wb');
      return;
    }
    const c = e.target.closest('input[data-wb-pool]');
    if (c) markRowDirty(c.closest('tr'), 'wb');
  };
  // 账号列铅笔改名：手改后加本地锁（上游同步跳过，不被手动全量刷新覆盖）；
  // 清空 = 解锁并从上游拉当前昵称恢复，所以 allowEmpty。
  tbody.onclick = e => {
    const pen = e.target.closest('.nick-pen');
    if (pen) startNickEdit(pen, { save: saveWbNickname, reload: loadWorkbuddyAccounts, allowEmpty: true });
  };
}
async function loadWorkbuddyProxyState() {
  try {
    const state = await workbuddyProxyAdminCall('GET', 'proxy');
    wbProxyCache = { bindings: state.bindings || {} };
  } catch (e) { /* account list still renders without the optional proxy list */ }
  return wbProxyCache;
}
function updateWorkbuddySummary() {
  const summary = _('wbSummary');
  if (!summary) return;
  const active = wbAccounts.filter(a => wbStatusInfo(a).cls === 'active').length;
  const cooling = wbAccounts.filter(a => wbStatusInfo(a).cls === 'cooldown').length;
  summary.textContent = wbAccounts.length + ' ' + T('accounts') + ' · ' + active + ' ' + T('active') + ' · ' + cooling + ' ' + T('cooling');
}
async function loadWorkbuddyAccounts() {
  const tbody = _('wbAccountsBody');
  if (tbody) tbody.innerHTML = '<tr><td colspan="10" class="empty">Loading...</td></tr>';
  try {
    await loadProxyListCache();
    const [overview] = await Promise.all([wbCall('overview'), loadWorkbuddyProxyState()]);
    wbAccounts = overview.accounts || [];
    renderWorkbuddyAccounts();
    updateWorkbuddySummary();
  } catch (e) {
    // WorkBuddy 子系统启动失败被禁用时 overview 路由不存在（404）：
    // 与"暂无账号"同态展示，不把装配失败透传成硬错误。
    if (String(e && e.message || '').indexOf('404') >= 0) {
      wbAccounts = [];
      renderWorkbuddyAccounts();
    } else if (tbody) {
      tbody.innerHTML = '<tr><td colspan="9" class="empty">' + esc(T('Failed to load WorkBuddy accounts: ') + e.message) + '</td></tr>';
    }
    updateWorkbuddySummary();
  }
}
function workbuddyBindingId(value) {
  return String(value ?? '').trim();
}
async function workbuddyProxyAdminCall(method, path, body) {
  const response = await api(method, '/workbuddy/' + path, body);
  return response.data !== undefined ? response.data : response;
}
// wbSnapshot 记录各账号渲染时的生效状态（启用 + 代理绑定显示值，绑定原始
// 数据在 wbProxyCache 里），"保存"按钮据此 diff 出真正需要提交的账号。
let wbSnapshot = {};
async function saveWorkbuddyEdits() {
  const rows = [...document.querySelectorAll('#wbAccountsBody tr')].filter(r => r.querySelector('.eg-input'));
  let changed = 0, failed = 0;
  for (const row of rows) {
    const mainInput = row.querySelector('.eg-input[data-eg="wb"][data-slot="main"]');
    if (!mainInput) continue;
    const uid = mainInput.dataset.egid;
    const main = mainInput.dataset.value || '';
    const backup = row.querySelector('.eg-input[data-eg="wb"][data-slot="backup"]').dataset.value || '';
    const cb = row.querySelector('input[data-wb-pool]');
    const accCb = row.querySelector('input[data-wb-account]');
    // 快照在渲染时记录（wbBindingCell 写入 main/backup 生效值），diff 与显示同口径。
    const snap = wbSnapshot[uid] || {};
    const snapMain = String(snap.main ?? '');
    const snapBackup = String(snap.backup ?? '');
    const snapAccount = snap.account !== undefined ? snap.account : true;
    const snapEnabled = snap.enabled !== undefined ? snap.enabled : true;
    // 目标态：账号开关以勾选框为准；反代开关只在账号开关最终开启时才可能变化
    //（账号关闭时勾选框禁用，持久值保持原样不提交——重开账号自动恢复）。
    const nextAccount = accCb ? accCb.checked : true;
    const nextPool = (nextAccount && cb) ? cb.checked : snapEnabled;
    try {
      if (main !== snapMain || backup !== snapBackup) {
        await workbuddyProxyAdminCall('POST', 'proxy/set', { uid: uid, main: main, backup: backup });
      }
      if (nextAccount !== snapAccount) {
        await api('POST', '/workbuddy/account-enabled', { uid: uid, enabled: nextAccount });
      }
      if (nextPool !== snapEnabled) {
        await api('POST', '/workbuddy/enabled', { uid: uid, enabled: nextPool });
      }
      if (main !== snapMain || backup !== snapBackup || nextAccount !== snapAccount || nextPool !== snapEnabled) changed++;
      row.classList.remove('tr-dirty');
    } catch (e) { failed++; }
  }
  clearDirtyMarks('wbAccountsBody', 'wbSaveBtn');
  if (failed) toast(T('Failed to save ') + failed + T(' account(s)'), 'error');
  else if (!changed) toast(T('No changes to save'), 'info');
  else toast(T('Saved ') + changed + T(' account edits'), 'success');
  await loadWorkbuddyAccounts();
}
function stageAllWbPool(enabled) {
  document.querySelectorAll('#wbAccountsBody input[data-wb-pool]').forEach(cb => {
    const snap = wbSnapshot[cb.dataset.wbPool];
    if (snap && snap.enabled === enabled) return;
    // 互锁：账号开关关闭的行不参与反代批量操作（勾选框本身禁用，后端也会拒绝）
    const acc = cb.closest('tr').querySelector('input[data-wb-account]');
    if (cb.disabled || (acc && !acc.checked)) return;
    cb.checked = enabled;
    markRowDirty(cb.closest('tr'), 'wb');
  });
}
function stageClearWorkbuddyProxyBindings() {
  if (!confirm(T('Clear proxy bindings on ALL accounts and disable them? Takes effect when you click Save.'))) return;
  document.querySelectorAll('#wbAccountsBody tr').forEach(row => {
    const main = row.querySelector('.eg-input[data-eg="wb"][data-slot="main"]');
    if (!main) return;
    let dirty = stageRowBindingClear(row, 'wb');
    const cb = row.querySelector('input[data-wb-pool]');
    if (cb && cb.checked) { cb.checked = false; dirty = true; }
    if (dirty) markRowDirty(row, 'wb');
  });
}
// 复位：丢弃账号表的全部暂存修改，按上次保存的状态重绘
function resetWorkbuddyEdits() {
  clearDirtyMarks('wbAccountsBody', 'wbSaveBtn');
  loadWorkbuddyAccounts();
}
// WB 批量动作的展示名（zh 下走 T() 翻译，键复用按钮文案词条）
const WB_BATCH_LABEL = { checkin_all: 'Check in all', travel_all: 'Pet patrol', activity_all: 'Activity all', keepalive_all: 'Refresh login', balance_all: 'Refresh balances' };
async function runWorkbuddyBatch(action) {
  const bodies = {
    checkin_all: {},
    travel_all: { accept_and_complete: true, force_complete: true, chain_cycles: 1 },
    activity_all: {},
    keepalive_all: {},
    balance_all: {}
  };
  const label = T(WB_BATCH_LABEL[action] || action);
  try {
    const result = await wbCall(action, {}, bodies[action] || {});
    toast(result.message || (label + T(' started')), 'success');
    await loadWorkbuddyAccounts();
  } catch (e) { toast(label + T(' failed: ') + e.message, 'error'); }
}
async function workbuddyActionFrom(button) {
  const action = button.dataset.wbAction;
  const id = workbuddyBindingId(button.dataset.wbId);
  if (action === 'details') { openWorkbuddyDetails(id, button.dataset.wbId); return; }
  if (action === 'delete' && !confirm(T('Delete this WorkBuddy account and its credentials?'))) return;
  const accountPath = 'accounts/' + encodeURIComponent(id);
  const actions = {
    delete: accountPath + '/remove'
  };
  if (action === 'test') { await workbuddyTestAccount(button, id); return; }
  if (!actions[action]) return;
  button.disabled = true;
  try {
    const result = await wbCall(actions[action], {}, {});
    toast(result.message || (T(action) + T(' completed')), 'success');
    await Promise.all([loadWorkbuddyAccounts(), loadWorkbuddyConfig()]);
  } catch (e) { toast(T(action) + T(' failed: ') + e.message, 'error'); }
  finally { button.disabled = false; }
}
/* ---- WB 单号可用性探测（与 key 平台 Test 同构：spinner + 结果 toast） ---- */
// wbBatchTestIds 供批量测试与单账号测试共用：逐个账号真实探测，汇总 toast。
async function batchTestWorkbuddyAccounts() {
  const btn = _('wbTestAllBtn');
  const ids = wbAccounts.map(wbRowId).filter(Boolean);
  if (!ids.length) { toast(T('No accounts to test'), 'info'); return; }
  const original = btn ? btn.innerHTML : '';
  if (btn) btn.disabled = true;
  let ok = 0, cool = 0, err = 0;
  try {
    for (let i = 0; i < ids.length; i++) {
      if (btn) btn.innerHTML = '<span class="loading"></span>' + (i + 1) + '/' + ids.length;
      try {
        const d = await wbCall('accounts/' + encodeURIComponent(ids[i]) + '/test', {}, {});
        if (d.status === 'active') ok++;
        else if (d.status === 'cooldown') cool++;
        else err++;
      } catch (e) { err++; }
    }
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = original; }
  }
  toast(T('Batch test done: ') + ids.length + T(' tested, ') + ok + T(' ok') +
    (cool ? T(', ') + cool + T(' cooling') : '') + (err ? T(', ') + err + T(' error') : ''),
    err ? 'warning' : 'success', 6000);
  loadWorkbuddyAccounts();
}

async function workbuddyTestAccount(button, id) { const original = button ? button.innerHTML : '';
  if (button) { button.disabled = true; button.innerHTML = '<span class="loading"></span>Testing'; }
  try {
    const d = await wbCall('accounts/' + encodeURIComponent(id) + '/test', {}, {});
    const ok = d.status === 'active';
    let msg = ok ? T('Test passed') : T('Test failed: ') + (d.error || d.status || '');
    if (d.latencyMs != null) msg += ' (' + d.latencyMs + 'ms)';
    if (ok && d.cooldownCleared) msg += T(' — cooldown cleared');
    else if (d.status === 'cooldown') msg += T(' — cooling');
    toast(msg, ok ? 'success' : 'error', 6000);
  } catch (e) {
    toast(T('Test failed: ') + e.message, 'error');
  } finally {
    if (button) { button.disabled = false; button.innerHTML = original; }
    loadWorkbuddyAccounts();
  }
}
async function refreshWorkbuddyAccounts() {
  try {
    const result = await wbCall('balance_all', {}, {});
    toast(result.message || T('Balances refreshed'), 'success');
    await loadWorkbuddyAccounts();
  } catch (e) { toast(T('Refresh failed: ') + e.message, 'error'); }
}

const WB_PK_COLORS = ['#4f8cff', '#25b08b', '#e8a33d', '#c96bd6', '#e2607a',
                      '#5aa9e6', '#8fbf3f', '#b58b5a', '#7d8fa8', '#d4785c'];
function wbPackageSources(packs) {
  const out = new Map();
  (packs || []).forEach(p => {
    const key = String(p.package_code || '') + '|' + String(p.name || T('(unnamed)'));
    const item = out.get(key) || {
      key: key, name: p.name || T('(unnamed)'), count: 0, remain: 0, size: 0, used: 0,
      minCreated: '', minEnd: ''
    };
    item.count += 1;
    item.remain += Number(p.remain || 0);
    item.size += Number(p.size || 0);
    item.used += Number(p.used || 0);
    const created = String(p.created_at || '').slice(0, 10);
    if (created && (!item.minCreated || created < item.minCreated)) item.minCreated = created;
    const end = String(p.end_time || '').slice(0, 10);
    if (end && (!item.minEnd || end < item.minEnd)) item.minEnd = end;
    out.set(key, item);
  });
  return Array.from(out.values()).sort((a, b) => b.size - a.size);
}
function wbPackageLabel(name) {
  return String(name || '').replace(/^CodeBuddy/, '');
}
let wbDetailsAccountId = null;
function openWorkbuddyDetails(id, label) {
  wbDetailsAccountId = workbuddyBindingId(id);
  _('wbDetailsWho').textContent = label || id;
  _('wbDetailsStatus').textContent = T('Querying points breakdown from upstream...');
  _('wbDetailsBody').innerHTML = '';
  showWorkbuddyDialog(_('wbDetailsDialog'));
  loadWorkbuddyDetails();
}
function closeWorkbuddyDetails() {
  wbDetailsAccountId = null;
  const dialog = _('wbDetailsDialog');
  if (dialog && dialog.open) dialog.close();
}
async function loadWorkbuddyDetails() {
  if (wbDetailsAccountId == null) return;
  try {
    const data = await wbCall('packages');
    const account = (data.accounts || []).find(a => String(a.uid) === String(wbDetailsAccountId));
    if (!account) throw new Error(T('No points data found for this account'));
    renderWorkbuddyDetails(account);
    _('wbDetailsStatus').textContent = T('Live from upstream · ') + (account.packages || []).length + T(' points packages');
  } catch (e) {
    _('wbDetailsStatus').textContent = T('Failed to read: ') + e.message;
    _('wbDetailsBody').innerHTML = '';
  }
}
function renderWorkbuddyDetails(a) {
  const body = _('wbDetailsBody');
  if (a.error) {
    body.innerHTML = '<div class="empty">' + T('Query failed: ') + esc(a.error) + '</div>';
    return;
  }
  const packs = a.packages || [];
  const sources = wbPackageSources(packs);
  const total = Math.max(1, Number(a.size || 0));
  const colorOf = key => WB_PK_COLORS[Math.max(0, sources.findIndex(s => s.key === key)) % WB_PK_COLORS.length];
  const mix = sources.map(s =>
    '<i style="width:' + (s.size / total * 100).toFixed(2) + '%;background:' + colorOf(s.key) + '" title="' +
    esc(wbPackageLabel(s.name)) + ' ' + esc(wbFmtTokens(s.size)) + '"></i>').join('');
  const legend = sources.map(s =>
    '<span><i style="background:' + colorOf(s.key) + '"></i>' + esc(wbPackageLabel(s.name)) +
    ' x' + s.count + ' · ' + esc(wbFmtTokens(s.size)) +
    (s.minCreated ? ' · ' + T('first issued ') + esc(s.minCreated.slice(5)) : '') + '</span>').join('');
  const rows = packs.map(p => {
    const key = String(p.package_code || '') + '|' + String(p.name || T('(unnamed)'));
    const sub = String(p.sub_product_code || '').replace(/^sp_tcaca_codebuddyide_?/, '') ||
      String(p.package_code || '').replace(/^TCACA_/, '');
    return '<tr><td><span style="display:inline-block;width:8px;height:8px;border-radius:2px;background:' +
      colorOf(key) + '"></span></td>' +
      '<td>' + esc(p.name || T('(unnamed)')) + (sub ? '<div class="hint" style="font-size:11px">' + esc(sub) + '</div>' : '') + '</td>' +
      '<td class="text-right">' + esc(wbFmtTokens(p.size)) + '</td>' +
      '<td class="text-right">' + esc(wbFmtTokens(p.remain)) + '</td>' +
      '<td class="text-right">' + esc(wbFmtTokens(p.used)) + '</td>' +
      '<td class="text-right">' + esc(String(p.created_at || '').slice(0, 16).replace('T', ' ') || '—') + '</td>' +
      '<td class="text-right">' + esc(String(p.end_time || '').slice(0, 10) || '—') + '</td></tr>';
  }).join('');
  body.innerHTML =
    '<div class="wb-pk-card"><div class="wb-pk-head"><div><div class="wb-pk-name">' +
      esc(a.nickname || a.uid) + '</div><div class="wb-pk-sub">' + esc(a.realm || '') + ' · ' + esc(a.uid) + '</div></div>' +
      '<div class="wb-pk-sub">' + T('Total ') + esc(wbFmtTokens(a.size)) + ' · ' + packs.length + T(' package(s)') + '</div></div>' +
      '<div class="wb-pk-big">' + esc(wbFmtTokens(a.remain)) + '</div>' +
      '<div class="wb-pk-sub">' + T('Remaining points · used ') + esc(wbFmtTokens(Math.max(0, Number(a.size || 0) - Number(a.remain || 0)))) + '</div>' +
      '<div class="wb-pk-mix">' + mix + '</div><div class="wb-pk-legend">' + legend + '</div></div>' +
    '<div class="table-wrap"><table><thead><tr><th></th><th>' + T('Package / source') + '</th><th class="text-right">' + T('Face value') + '</th>' +
      '<th class="text-right">' + T('Remaining') + '</th><th class="text-right">' + T('Used') + '</th><th class="text-right">' + T('Granted') + '</th>' +
      '<th class="text-right">' + T('Expires') + '</th></tr></thead><tbody>' +
      (rows || '<tr><td colspan="7" class="empty">' + T('No points packages') + '</td></tr>') + '</tbody></table></div>';
}

let wbTodoTimer = null;
let wbTodoSeq = 0;
function wbTodoConcurrency() {
  const value = Number(_('wbTodoConcurrency') && _('wbTodoConcurrency').value);
  return Math.min(3, Math.max(1, Number.isFinite(value) ? value : 1));
}
function openWorkbuddyTodoScan() {
  wbTodoSeq = 0;
  _('wbTodoStatus').textContent = T('Pick a concurrency, then click "Scan tasks"');
  _('wbTodoBody').innerHTML = '';
  showWorkbuddyDialog(_('wbTodoDialog'));
}
function closeWorkbuddyTodoScan() {
  if (wbTodoTimer) { clearTimeout(wbTodoTimer); wbTodoTimer = null; }
  wbTodoSeq = 0;
  const dialog = _('wbTodoDialog');
  if (dialog && dialog.open) dialog.close();
}
function renderWorkbuddyTodoAccounts(data) {
  const accounts = data.accounts || [];
  const groups = accounts.filter(a => (a.growth || []).length || a.growth_error);
  if (!groups.length) {
    _('wbTodoBody').innerHTML = '<div class="empty">' + T('No pending tasks for all accounts') + '</div>';
    return;
  }
  _('wbTodoBody').innerHTML = groups.map(a => {
    const rows = (a.growth || []).map(t => {
      const progress = t.target ? (t.current || 0) + ' / ' + t.target : '—';
      const title = t.title || t.task_code || '';
      const desc = [t.task_desc, t.description].filter(Boolean).join('\n');
      const reward = [
        Number(t.credit || 0) > 0 ? T('Points') + ' ' + t.credit : '',
        Number(t.energy || 0) > 0 ? T('Energy') + ' ' + t.energy : ''
      ].filter(Boolean).join(' · ');
      const tip = [
        T('Task: ') + title,
        T('Description: ') + (desc || '—'),
        T('Progress: ') + progress,
        T('Reward: ') + (reward || '—')
      ].join('\n');
      return '<div class="wb-todo-row" title="' + esc(tip) + '"><div class="nm"><div>' + esc(title) +
        '</div><div class="id">' + esc(t.task_code || '') + '</div></div><span class="model-tag">' + T('Pending') + '</span>' +
        '<span class="wb-usage">' + esc(progress) + '</span></div>';
    }).join('');
    const err = a.growth_error ? '<div class="hint" style="color:var(--danger)">' + T('Scan failed: ') + '' + esc(a.growth_error) + '</div>' : '';
    return '<details class="wb-todo-group"><summary class="wb-todo-title">' + esc(a.nickname || a.uid) +
      ' <span class="auto-pill">' + esc(a.uid || '') + ' · ' + T(' item(s)') + '</span></summary>' + rows + err + '</details>';
  }).join('');
}
async function scanWorkbuddyTodos() {
  const concurrency = wbTodoConcurrency();
  const button = _('wbTodoRunAll');
  const scanButton = _('wbTodoScan');
  if (button) button.disabled = true;
  if (scanButton) scanButton.disabled = true;
  _('wbTodoStatus').textContent = T('Scanning with concurrency ') + concurrency + T(' scanning tasks...');
  _('wbTodoBody').innerHTML = '<div class="empty">' + T('Scanning...') + '</div>';
  try {
    const data = await wbCall('tasks/scan_all', {}, { concurrency: concurrency });
    renderWorkbuddyTodoAccounts(data);
    _('wbTodoStatus').textContent = T('Scan complete: ') + (data.pending_count || 0) + T(' pending item(s)');
  } catch (e) {
    _('wbTodoStatus').textContent = T('Scan failed: ') + e.message;
    _('wbTodoBody').innerHTML = '';
  } finally {
    if (button) button.disabled = false;
    if (scanButton) scanButton.disabled = false;
  }
}
function renderWorkbuddyQueue(data) {
  const items = data.items || [];
  if (!items.length) return;
  const groups = new Map();
  items.forEach(it => {
    const key = String(it.uid || '');
    if (!groups.has(key)) groups.set(key, { uid: key, nickname: it.nickname || key, items: [] });
    groups.get(key).items.push(it);
  });
  const labels = { pending: T('Claimed'), running: T('In progress'), done: T('Done'), skipped: T('Skipped'), error: T('Failed') };
  _('wbTodoBody').innerHTML = Array.from(groups.values()).map(g =>
    '<details class="wb-todo-group"><summary class="wb-todo-title">' + esc(g.nickname) +
      ' <span class="auto-pill">' + esc(g.uid) + ' · ' + T(' item(s)') + '</span></summary>' +
      g.items.map(it => {
        const title = it.title || it.code || '';
        const tip = [
          T('Task: ') + title,
          T('Description: ') + (it.desc || '—'),
          it.progress ? T('Progress: ') + it.progress : '',
          it.message ? T('Result: ') + it.message : ''
        ].filter(Boolean).join('\n');
        const sub = [it.code, it.progress, it.message].filter(Boolean).join(' · ');
        return '<div class="wb-todo-row" title="' + esc(tip) + '"><div class="nm"><div>' + esc(title) +
          '</div><div class="id">' + esc(sub) + '</div></div><span class="model-tag">' +
          esc(labels[it.status] || it.status || '') + '</span></div>';
      }).join('') + '</details>').join('');
}
async function pollWorkbuddyQueue() {
  const seq = wbTodoSeq;
  if (!seq) return;
  try {
    const data = await wbCall('tasks/queue');
    if (seq !== wbTodoSeq) return;
    renderWorkbuddyQueue(data);
    const done = (data.items || []).filter(it => ['done', 'skipped', 'error'].includes(it.status)).length;
    _('wbTodoStatus').textContent = (data.running ? T('Running') : T('Finished')) + T(': ') + done + ' / ' + (data.total || 0);
    if (data.running) {
      wbTodoTimer = setTimeout(pollWorkbuddyQueue, 1500);
    } else {
      wbTodoTimer = null;
      await loadWorkbuddyAccounts();
    }
  } catch (e) {
    _('wbTodoStatus').textContent = T('Failed to read queue: ') + e.message;
  }
}
async function runWorkbuddyAllTodos() {
  const concurrency = wbTodoConcurrency();
  if (!confirm(T('Run all listed tasks with concurrency ') + concurrency + T(' (tasks run serially within each account, auto-claim enabled). Confirm?'))) return;
  const button = _('wbTodoRunAll');
  const scanButton = _('wbTodoScan');
  if (button) button.disabled = true;
  if (scanButton) scanButton.disabled = true;
  try {
    const result = await wbCall('tasks/run_queue', {}, { concurrency: concurrency, growth: true });
    if (!result.started) {
      toast(result.message || T('No pending tasks'), 'info');
      return;
    }
    wbTodoSeq = result.seq || 0;
    toast(T('Queue started: ') + result.total + T(' item(s) (concurrency ') + concurrency + T(')'), 'success');
    _('wbTodoStatus').textContent = T('Queue started, running...');
    wbTodoTimer = setTimeout(pollWorkbuddyQueue, 800);
  } catch (e) {
    toast(T('Failed to run: ') + e.message, 'error');
  } finally {
    if (button) button.disabled = false;
    if (scanButton) scanButton.disabled = false;
  }
}

function openWorkbuddyAdd() {
  const dialog = _('wbAddDialog');
  showWorkbuddyDialog(dialog);
  _('wbAddStatus').textContent = 'Choose a realm and request an authorization link.';
  _('wbOAuthUrl').value = '';
  _('wbStartLogin').disabled = false;
}
function closeWorkbuddyAdd() {
  if (wbOAuthTimer) { clearTimeout(wbOAuthTimer); wbOAuthTimer = null; }
  const dialog = _('wbAddDialog');
  if (dialog && dialog.open) dialog.close();
}
async function startWorkbuddyOAuth() {
  const button = _('wbStartLogin');
  button.disabled = true;
  button.textContent = T('Requesting...');
  _('wbAddStatus').textContent = 'Requesting authorization link...';
  try {
    const payload = await wbCall('login/start', {}, { site: _('wbRealm').value });
    const url = payload.url || '';
    _('wbOAuthUrl').value = url;
    _('wbAddStatus').textContent = url ? 'Open the link and complete the browser login.' : 'Authorization session created.';
    if (url) wbOAuthTimer = setTimeout(() => pollWorkbuddyOAuth(payload.state), 1200);
  } catch (e) {
    _('wbAddStatus').textContent = T('OAuth failed: ') + e.message;
  } finally {
    button.disabled = false;
    button.textContent = 'Get authorization link';
  }
}
async function pollWorkbuddyOAuth(oauthId) {
  if (!oauthId) return;
  try {
    const payload = await wbCall('login/poll', { state: oauthId });
    if (payload.done) {
      _('wbAddStatus').textContent = T('Account added: ') + (payload.nickname || payload.uid || '');
      toast('WorkBuddy account added', 'success');
      await loadWorkbuddyAccounts();
      return;
    }
    if (payload.status === 'failed' || payload.error) throw new Error(payload.error || payload.status || 'failed');
    wbOAuthTimer = setTimeout(() => pollWorkbuddyOAuth(oauthId), 1500);
  } catch (e) { _('wbAddStatus').textContent = T('OAuth failed: ') + e.message; }
}
function copyWorkbuddyOAuthUrl() {
  const value = _('wbOAuthUrl').value;
  if (!value) return;
  copyText(value, 'Authorization link copied', _('wbOAuthUrl'));
}
function openWorkbuddyOAuthUrl() {
  const value = _('wbOAuthUrl').value;
  if (value) window.open(value, '_blank', 'noopener');
}
async function importWorkbuddyAccounts(input) {
  const file = input.files && input.files[0];
  if (!file) return;
  try {
    const form = new FormData();
    form.append('file', file, file.name);
    const response = await fetch(API + '/workbuddy/accounts/import', { method: 'POST', body: form });
    const result = await response.json().catch(() => ({}));
    if (!response.ok || result.ok === false) throw new Error(result.error || response.statusText || 'upload failed');
    toast('WorkBuddy account imported', 'success');
    await loadWorkbuddyAccounts();
  } catch (e) { toast(T('Import failed: ') + e.message, 'error'); }
  input.value = '';
}

async function loadWorkbuddyConfig() {
  const form = _('wbConfigForm');
  if (!form) return;
  try {
    const result = await wbCall('config');
    const config = result.config || result;
    form.querySelectorAll('[data-wb-path]').forEach(el => {
      const value = wbDig(config, el.dataset.wbPath);
      if (el.type === 'checkbox') el.checked = !!value;
      else if (value === undefined || value === null) el.value = '';
      else if (Array.isArray(value)) el.value = value.join(', ');
      else el.value = String(value);
    });
    // 倍率筛选同样来自同一份配置：网关设置页的下拉与 WB 配置页共用一份真值
    // （saveWorkbuddyConfig 之后这里把新阈值同步进内存）。
    syncWorkbuddyRateFilter(config);
    _('wbConfigNote').textContent = T('loaded');
  } catch (e) {
    _('wbConfigNote').textContent = T('load failed');
    toast(T('WorkBuddy config load failed: ') + e.message, 'error');
  }
}
// syncWorkbuddyRateFilter 从服务端配置同步倍率筛选阈值；config 省略时自行拉取。
// 非法/缺失值回落默认 0.2（与 wbconfig.Default()、and 服务端兜底同一数值）。
async function syncWorkbuddyRateFilter(config) {
  let cfg = config;
  if (!cfg) {
    try {
      const result = await wbCall('config');
      cfg = result.config || result;
    } catch (e) { return; }
  }
  const raw = Number(wbDig(cfg, 'pool.model_rate_filter'));
  const next = WB_RATE_OPTIONS.includes(raw) ? raw : 0.2;
  if (next !== workbuddyRateFilter) workbuddyRateFilter = next;
  return next;
}
function collectWorkbuddyConfig() {
  const out = {};
  _('wbConfigForm').querySelectorAll('[data-wb-path]').forEach(el => {
    const path = el.dataset.wbPath;
    const type = el.dataset.wbType || 'string';
    if (el.type === 'checkbox') { wbPut(out, path, el.checked); return; }
    const raw = (el.value || '').trim();
    if (!raw && !el.dataset.wbSendEmpty) return;
    if (type === 'ints') {
      const values = raw.split(/[\s,，]+/).map(Number).filter(Number.isFinite);
      if (values.length) wbPut(out, path, values);
      return;
    }
    if (type === 'number') {
      if (raw === '') return;
      const value = Number(raw);
      if (Number.isFinite(value)) wbPut(out, path, value);
      return;
    }
    wbPut(out, path, raw);
  });
  return out;
}
async function saveWorkbuddyConfig() {
  const button = _('wbSaveConfig');
  button.disabled = true;
  button.textContent = T('Saving...');
  try {
    const result = await wbCall('config', {}, collectWorkbuddyConfig());
    const restart = Array.isArray(result.restart_required) ? result.restart_required.length : 0;
    toast(restart ? T('WorkBuddy config saved (') + restart + T(' restart items)') : T('WorkBuddy config saved'), 'success');
    await loadWorkbuddyConfig();
    // 倍率阈值等影响目录的配置随保存热刷新（目录区不再内嵌独立筛选下拉）
    loadModels();
  } catch (e) { toast(T('WorkBuddy config save failed: ') + e.message, 'error'); }
  finally { button.disabled = false; button.textContent = T('Save configuration'); }
}

(_('wbStartLogin') || {}).onclick = startWorkbuddyOAuth;
(_('wbConfigForm') || {}).addEventListener('submit', ev => { ev.preventDefault(); saveWorkbuddyConfig(); });

// ========== API helper ==========
async function api(method, path, body) {
  const opts = { method, headers: {} };
  if (body) { opts.headers['Content-Type'] = 'application/json'; opts.body = JSON.stringify(body); }
  const res = await fetch(API + path, opts);
  if (res.status === 401 && path !== '/login') { location.reload(); return new Promise(() => {}); }
  const data = await res.json();
  if (!data.success && data.error) throw new Error(data.error);
  return data;
}

// ========== Dashboard ==========
// setDash 平台概览卡片：统一"N 账号 · N 可用 · N 冷却中"行文（OpenCode 单位为 key）
function setDash(id, unit, total, active, cooling) {
  const el = _(id);
  if (!el) return;
  el.textContent = total + ' ' + T(unit) + ' · ' + active + ' ' + T('active') + ' · ' + cooling + ' ' + T('cooling');
}
// setReq 请求统计块：统一"N 请求 · N 错误 · 平均 N ms"
function setReq(id, s) {
  const el = _(id);
  if (!el) return;
  s = s || {};
  el.textContent = (s.requests || 0) + ' ' + T('requests') + ' · ' + (s.errors || 0) + ' ' + T('errors') + ' · ' + T('Avg ') + Math.round(s.avg_ms || 0) + 'ms';
}
// loadStats 仪表盘三平台概览：Cline 走 /stats（含 expired 但概览行不展示），
// OpenCode 取 /opencode/config 的 keyStates（可用=路由启用），WorkBuddy 取
// 子系统 overview 的池计数（可用=healthy）。子系统被禁用时该卡片保持 "-"。
async function loadStats() {
  try {
    const d = await api('GET', '/stats');
    const s = d.data;
    setDash('dashCline', 'accounts', s.total, s.active, s.cooldown);
  } catch (e) { /* ignore */ }
  try {
    const d = await api('GET', '/opencode/config');
    const ks = (d.data && d.data.keyStates) || [];
    setDash('dashOc', 'keys', ks.length, ks.filter(k => k.routingEnabled !== false).length, ks.filter(k => k.cooling).length);
  } catch (e) { /* ignore */ }
  try {
    const ov = await wbCall('overview');
    setDash('dashWb', 'accounts', ov.total, ov.healthy, ov.cooling);
  } catch (e) { /* ignore */ }
  try {
    const d = await api('GET', '/openrouter/config');
    const ks = (d.data && d.data.keyStates) || [];
    setDash('dashOr', 'keys', ks.length, ks.filter(k => k.routingEnabled !== false).length, ks.filter(k => k.cooling).length);
  } catch (e) { /* ignore */ }
  try {
    const d = await api('GET', '/trae/config');
    const accs = (d.data && d.data.accounts) || [];
    setDash('dashTrae', 'accounts', accs.length, accs.filter(a => a.routingEnabled !== false).length, accs.filter(a => a.cooling).length);
  } catch (e) { /* ignore */ }
  try {
    const d = await api('GET', '/amd/config');
    const ks = (d.data && d.data.keyStates) || [];
    setDash('dashAmd', 'keys', ks.length, ks.filter(k => k.routingEnabled !== false).length, ks.filter(k => k.cooling).length);
  } catch (e) { /* ignore */ }
  try {
    const d = await api('GET', '/tokenharbor/config');
    const ks = (d.data && d.data.keyStates) || [];
    setDash('dashTh', 'keys', ks.length, ks.filter(k => k.routingEnabled !== false).length, ks.filter(k => k.cooling).length);
  } catch (e) { /* ignore */ }
  try {
    const d = await api('GET', '/other/config');
    const ps = (d.data && d.data.platforms) || [];
    let n = 0, routed = 0, cooling = 0;
    ps.forEach(p => {
      const ks = p.keyStates || [];
      n += ks.length;
      routed += ks.filter(k => k.routingEnabled !== false).length;
      cooling += ks.filter(k => k.cooling).length;
    });
    setDash('dashOther', 'keys', n, routed, cooling);
  } catch (e) { /* ignore */ }
  try {
    const d = await api('GET', '/request-stats');
    const rs = (d.data && d.data.routes) || {};
    setReq('reqCline', rs.cline);
    setReq('reqTrae', rs.trae);
    setReq('reqZen', rs.zen);
    setReq('reqWb', rs.workbuddy);
    setReq('reqOr', rs.openrouter);
    setReq('reqAmd', rs.amd);
    setReq('reqTh', rs.tokenharbor);
    setReq('reqOther', rs.other);
    const hint = _('reqStatsHint');
    if (hint && d.data && d.data.since) hint.textContent = T('Counted since ') + new Date(d.data.since).toLocaleString();
  } catch (e) { /* ignore */ }
}

// ========== Accounts ==========
// ========== 账号列表拖拽排序（Account pool 三菜单共用） ==========
// enableRowDragReorder 给账号表的 tbody 装上 HTML5 原生行拖拽：
// - dragover 实时把拖拽行插到目标位置（即时生效的视觉反馈，无需重建 DOM）；
// - 松手后按 tr[data-drag-id] 收集新顺序提交 submit(ids)，成功 toast 后 reload()
//   （reload 从后端重拉，保证 index/轮转游标等派生状态与磁盘一致），失败 reload 回滚；
// - 行内有未保存的暂存修改（.tr-dirty）时禁止起拖：OpenCode key 表的暂存协议
//   按 index 定位，重排会让未提交的 index 指向错误的 key。
// 拖拽从单元格空白/文本处起拖；input/select/button/label/a 上不起拖，避免
// 干扰下拉、勾选与按钮点击。空态行（无 data-drag-id）不参与。
function enableRowDragReorder(tbodyId, submit, reload) {
  const tb = _(tbodyId);
  if (!tb) return;
  tb.addEventListener('dragstart', e => {
    const tr = e.target.closest('tr[data-drag-id]');
    if (!tr || e.target.closest('input,select,textarea,button,a,label')) { e.preventDefault(); return; }
    if (tb.querySelector('tr.tr-dirty')) {
      e.preventDefault();
      toast(T('Save or discard staged edits before reordering'), 'warning');
      return;
    }
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', tr.dataset.dragId);
    tr.classList.add('row-dragging');
  });
  tb.addEventListener('dragover', e => {
    const dragging = tb.querySelector('tr.row-dragging');
    if (!dragging) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = 'move';
    const over = e.target.closest('tr[data-drag-id]');
    if (!over || over === dragging) return;
    const rect = over.getBoundingClientRect();
    const before = (e.clientY - rect.top) < rect.height / 2;
    tb.insertBefore(dragging, before ? over : over.nextSibling);
  });
  tb.addEventListener('drop', e => e.preventDefault());
  tb.addEventListener('dragend', async () => {
    const dragging = tb.querySelector('tr.row-dragging');
    if (!dragging) return;
    dragging.classList.remove('row-dragging');
    const ids = [...tb.querySelectorAll('tr[data-drag-id]')].map(tr => tr.dataset.dragId);
    try {
      await submit(ids);
      toast(T('Order saved'), 'success');
    } catch (err) {
      toast(T('Reorder failed: ') + err.message, 'error');
    }
    if (reload) reload(); // 成功=对齐派生状态；失败=回滚
  });
}

// ========== 账号列昵称（cline 账号表 + WorkBuddy 账号表共用）==========
// 昵称仅是显示名：面板账号列与日志标签用，不改任何路由/选号行为。
// 铅笔行内改名：点击铅笔 → 名称变输入框，Enter/✓ 保存，Esc/✕ 取消；
// 保存成功后由 opts.reload() 重拉列表对齐。
const NICK_PEN_SVG = '<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M17 3a2.85 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z"/></svg>';
// nickPen 生成铅笔按钮；current 是可编辑值（data-nick-current），id 走 data-nick-id。
const nickPen = (id, current, title) =>
  '<button type="button" class="nick-pen" data-nick-id="' + esc(id) + '" data-nick-current="' + esc(current) + '" title="' + esc(title) + '" aria-label="' + esc(title) + '">' + NICK_PEN_SVG + '</button>';

// startNickEdit 在铅笔所在的 .nick-wrap 内展开行内编辑。opts:
//   save(id, value) -> Promise  保存 API（失败抛错）
//   reload()                    成功后重拉列表
//   allowEmpty                  空值语义：cline true = 清除昵称恢复显示邮箱；
//                               wb false = 上游昵称兜底下不允许清空
function startNickEdit(pen, opts) {
  const wrap = pen.closest('.nick-wrap');
  if (!wrap || wrap.querySelector('.nick-input')) return;
  const id = pen.dataset.nickId;
  const current = pen.dataset.nickCurrent || '';
  const nameEl = wrap.querySelector('.nick-name');
  nameEl.style.display = 'none';
  pen.style.display = 'none';
  const input = document.createElement('input');
  input.className = 'nick-input';
  input.maxLength = 64;
  input.value = current;
  input.placeholder = T('Nickname');
  const okBtn = document.createElement('button');
  okBtn.type = 'button'; okBtn.className = 'nick-ok'; okBtn.textContent = '✓'; okBtn.title = T('Save');
  const noBtn = document.createElement('button');
  noBtn.type = 'button'; noBtn.className = 'nick-no'; noBtn.textContent = '✕'; noBtn.title = T('Cancel');
  wrap.append(input, okBtn, noBtn);
  input.focus();
  if (current) input.select();
  const cleanup = () => {
    input.remove(); okBtn.remove(); noBtn.remove();
    nameEl.style.display = ''; pen.style.display = '';
  };
  let busy = false;
  const doSave = async () => {
    if (busy) return;
    const v = input.value.trim();
    if (!v && !opts.allowEmpty) { toast(T('Nickname cannot be empty'), 'warning'); input.focus(); return; }
    if (v === current) { cleanup(); return; } // 未变 = 视为取消，不打 API
    busy = true;
    try {
      await opts.save(id, v);
      toast(T('Nickname saved'), 'success');
      cleanup();
      busy = false;
      opts.reload();
    } catch (err) {
      busy = false;
      toast(T('Failed to save nickname: ') + err.message, 'error');
    }
  };
  const doCancel = () => { if (!busy) cleanup(); };
  okBtn.onclick = doSave;
  noBtn.onclick = doCancel;
  input.onkeydown = e => {
    if (e.key === 'Enter') doSave();
    else if (e.key === 'Escape') doCancel();
  };
}
const saveClineNickname = (accountId, nickname) => api('POST', '/accounts/nickname', { accountId, nickname });
const saveWbNickname = (uid, nickname) => api('POST', '/workbuddy/nickname', { uid, nickname });

async function loadAccounts() {
  try {
    await loadProxyListCache();
    const d = await api('GET', '/accounts');
    const list = d.data.accounts;
    const tbody = _('accountTableBody');
    // 标题下统计：账号 · 可用 · 冷却中（与 WorkBuddy 页摘要同款）
    const summary = _('accSummary');
    if (summary) {
      const active = (list || []).filter(a => a.status === 'active').length;
      const cooling = (list || []).filter(a => a.status === 'cooldown').length;
      summary.textContent = (list || []).length + ' ' + T('accounts') + ' · ' + active + ' ' + T('active') + ' · ' + cooling + ' ' + T('cooling');
    }
    if (!list || list.length === 0) {
      tbody.innerHTML = '<tr><td colspan="8" class="empty">No accounts yet</td></tr>';
      return;
    }
    const sn = { active: 'Active', cooldown: 'Cooldown', expired: 'Expired' };
    accSnapshot = {};
    tbody.innerHTML = list.map((a, i) => {
      const lu = a.lastUsed ? esc(fmtLastUsed(a.lastUsed)) : '-';
      // Cooldown label: show estimated recovery time
      let statusExtra = '';
      if (a.status === 'cooldown') {
        const until = a.cooldownUntil ? fmtShort(a.cooldownUntil) : '';
        statusExtra = until ? '<div style="font-size:10px;color:var(--text3);margin-top:2px;white-space:nowrap" title="' + esc(fmtWhen(a.cooldownUntil)) + '">' + T('Recovers ') + esc(until) + '</div>' : '';
      }
      // Proxy binding cells: main/backup comboboxes (Global / Direct / proxies
      // with type-ahead) saved on change. A binding that no longer exists in
      // the pool gets a stale marker — isolation keeps skipping the account
      // until it is fixed. The direct sentinel is never stale.
      // omitempty 序列化下未绑定账号没有 proxyMain/proxyBackup 键，归一化为 ''。
      const pMain = a.proxyMain || '';
      const pBackup = a.proxyBackup || '';
      const isStale = proxyStale;
      const stale = isStale(pMain) || isStale(pBackup);
      // 主 = 全局/直连 时备跟随锁定（渲染期同样生效）；备用代理开关关闭时同理。
      const backupLocked = !backupProxyOn || pMain === '' || pMain === EGRESS_DIRECT;
      const backupVal = backupLocked ? pMain : pBackup;
      const bindCell = '<td style="white-space:nowrap">' +
        egComboHTML('acc', a.accountId, 'main', pMain, T('Main exit — used whenever available')) + ' ' +
        egComboHTML('acc', a.accountId, 'backup', backupVal, T('Backup exit — used when main is cooling/removed'), backupLocked) +
        (stale ? ' <span title="' + esc(T('Bound proxy is no longer in the proxy pool — skipped until fixed')) + '" style="color:var(--danger)">⚠</span>' : '') +
        '</td>';
      // PoolEnabled 与账号状态是两个维度：未设置时按启用渲染，显式 false
      // 才退出主选号；勾选改动先暂存，由"保存"按钮统一提交。
      const poolEnabled = a.poolEnabled !== false;
      accSnapshot[a.accountId] = { main: pMain, backup: backupVal, enabled: poolEnabled };
      const poolCell = '<td style="text-align:center">' +
        '<input type="checkbox" data-pool-enabled="' + esc(a.accountId) + '"' +
        (poolEnabled ? ' checked' : '') +
        ' title="' + esc(T('Participates in request routing')) + '" aria-label="' + esc(T('Participates in request routing')) + '">' +
        '</td>';
      const st = esc(a.status);
      // 账号列：昵称优先（设了昵称时主行显示昵称、小字保留邮箱），铅笔行内改名。
      // 空昵称 = 清除（恢复显示邮箱），所以 allowEmpty。铅笔回显当前**显示值**
      // （昵称或邮箱），在原值基础上改而不是从空白开始。
      const nick = a.nickname || '';
      const displayName = nick || a.email || '(no email)';
      return '<tr draggable="true" data-drag-id="' + esc(a.accountId) + '">' +
        '<td class="mono" style="font-size:11px;color:var(--text3)">#' + (i + 1) + '</td>' +
        '<td><div class="nick-cell"><div class="nick-wrap">' +
          '<span class="nick-name" title="' + esc(displayName) + '">' + esc(displayName) + '</span>' +
          nickPen(a.accountId, displayName, T('Edit nickname')) +
        '</div>' +
        (nick ? '<div class="nick-sub" title="' + esc(a.email) + '">' + esc(a.email) + '</div>' : '') +
        '</div></td>' +
        '<td><span class="status ' + st + '"><span class="status-dot ' + st + '"></span>' + (sn[a.status] || st) + '</span>' + statusExtra + '</td>' +
          '<td title="' + T('Today') + ' ' + fmtNum(a.tokensToday) + ' / ' + T('total') + ' ' + fmtNum(a.tokensTotal) + ' ' + T('tokens (exact when upstream returns usage, otherwise estimated)') + '">' + fmtTokens(a.tokensToday) + ' / ' + fmtTokens(a.tokensTotal) + '</td>' +
        '<td class="mono" style="font-size:11px">' + lu + '</td>' +
        bindCell +
        '<td style="white-space:nowrap;text-align:center">' +
          // Action buttons dispatch via data-* attributes + delegated listeners:
          // inline onclick string concatenation gets HTML-decoded before JS parsing,
          // so even escaped quotes could be bypassed
          '<button class="btn btn-sm" data-act="test" data-acc="' + esc(a.accountId) + '" title="Test whether the account works (success clears cooldown/expired state)">Test</button> ' +
          '<button class="btn btn-sm" data-act="reset" data-acc="' + esc(a.accountId) + '" title="Probe the rate limit; if still limited, stays cooling with a recovery time">Reset</button> ' +
          '<button class="btn btn-sm btn-danger" data-act="delete" data-acc="' + esc(a.accountId) + '" title="Delete">Delete</button>' +
        '</td>' +
        poolCell +
        '</tr>';
    }).join('');
    tbody.onchange = e => {
      const input = e.target.closest('input[data-pool-enabled]');
      if (!input) return;
      // 暂存模型：勾选改动不直接落盘，行高亮，由"保存"按钮统一提交
      markRowDirty(input.closest('tr'), 'acc');
    };
    tbody.onclick = e => {
      const pen = e.target.closest('.nick-pen');
      if (pen) { startNickEdit(pen, { save: saveClineNickname, reload: loadAccounts, allowEmpty: true }); return; }
      const b = e.target.closest('button[data-act]');
      if (!b) return;
      const id = b.dataset.acc;
      if (b.dataset.act === 'test') testAccount(id, b);
      else if (b.dataset.act === 'reset') resetAccount(id, b);
      else if (b.dataset.act === 'delete') deleteAccount(id);
    };
  } catch (e) { toast(T('Failed to load accounts: ') + e.message, 'error'); }
}

// ========== Proxy binding (account isolation) ==========
// 代理池列表缓存：账号表与 zen key 表的绑定下拉共用。value 是完整代理 URL
//（保存需要，#别名随行持久化），展示名优先用别名，无别名时打码凭据段。
let proxyListCache = [];
// 备注名（#fragment）是代理的身份标识：绑定与池行的备注名相同即视为同一
// 代理——端口/凭据变化不触发 stale 警示（2026-10-18 用户定）。无备注的行
// 仍按精确 URI 匹配，避免任意无备注 URI 互通。
const proxyAliasOf = v => {
  if (!v) return '';
  const i = v.lastIndexOf('#');
  return i >= 0 ? v.slice(i + 1).trim() : '';
};
const proxyStale = v => {
  if (!v || v === EGRESS_DIRECT) return false;
  const a = proxyAliasOf(v);
  if (!a) return !proxyListCache.includes(v);
  return !proxyListCache.some(p => proxyAliasOf(p) === a);
};
// backupProxyOn 备用代理开关（代理池页，持久化在 proxy-config.json 里，默认关闭）。
// 关闭时备用槽整体失效：下拉只读并强制跟随主槽，保存后该身份只剩主出口。
// 默认 false 与后端一致 —— 首次渲染就是关闭态，避免开关未拉到就先解锁。
let backupProxyOn = false;
async function loadProxyListCache() {
  try {
    const d = await api('GET', '/opencode/config');
    proxyListCache = (d.data.proxies || []).slice();
    backupProxyOn = d.data.backupProxyEnabled === true;
    proxyExclusions = d.data.proxyExclusions || {};
    proxyBindCaps = d.data.proxyBindCaps || {};
    syncProxyHealthCache((d.data.runtime || {}).proxyHealth);
  } catch (e) { /* keep previous cache */ }
}
const maskProxyLabel = p => String(p || '').replace(/\/\/[^@/]*@/, '//***@');
// 每代理"最近在线率"缓存：与代理池页健康表同源（/opencode/config 的
// runtime.proxyHealth）。快照里的 proxy 是打码 URL，与 proxyListCache 的
// 完整 URL 对不上，两侧都剥掉凭据段后再以剩余部分作 key 对齐。
//
// 浏览器本地留存层（用户拍板 2026-10-11）：localStorage 持久化一份合并快照。
// **展示行始终与后端池一致**（健康表只列当前池内代理，暂离代理不显示）；
// **数值只加不删**——后端新增的代理加入快照；后端编辑清零/代理暂离归来时
// 后端读数为空，显示留存值兜底；后端有真实数据时正常跟随后端；本地手动
// 改过的行（m 标记，见 proxyRateSet）不被后端覆盖。纯展示层：不回传后端，
// 不影响路由与真实统计。
let proxyHealthByProxy = {};
let proxyHealthBackendRows = [];
const PROXY_HEALTH_STORE_KEY = 'afreeProxyHealthStore';
const proxyHealthKey = p => String(p || '').replace(/\/\/[^@/]*@/, '//');
function loadProxyHealthStore() {
  try { return JSON.parse(localStorage.getItem(PROXY_HEALTH_STORE_KEY) || '{}') || {}; }
  catch (e) { return {}; }
}
function saveProxyHealthStore() {
  try { localStorage.setItem(PROXY_HEALTH_STORE_KEY, JSON.stringify(proxyHealthByProxy)); } catch (e) { /* 隐私模式存不下就算了 */ }
}
function syncProxyHealthCache(rows) {
  proxyHealthBackendRows = (rows || []).filter(r => r && r.proxy);
  proxyHealthByProxy = loadProxyHealthStore();
  proxyHealthBackendRows.forEach(r => {
    const k = proxyHealthKey(r.proxy);
    const old = proxyHealthByProxy[k];
    if (old && old.m) return;                 // 手改行不被后端覆盖
    if (old && !(r.attempts > 0)) return;     // 后端无数据（编辑清零/暂离归来）→ 保留留存值
    proxyHealthByProxy[k] = Object.assign({}, r);
  });
  saveProxyHealthStore();
}
// 清空按钮（代理池页健康表右上）：抹掉本浏览器留存的快照（含手动改动与
// 已下线代理的行），再用最近一次后端快照重新合并——表格回到后端当前状态。
// 后端统计本身不受任何影响。
function clearProxyHealthStore() {
  if (!confirm(T('Clear the browser-stored online-rate data? Backend stats are untouched.'))) return;
  try { localStorage.removeItem(PROXY_HEALTH_STORE_KEY); } catch (e) { /* ignore */ }
  syncProxyHealthCache(proxyHealthBackendRows);
  renderProxyHealth(Object.values(proxyHealthByProxy));
  document.querySelectorAll('.eg-input').forEach(el => egSyncBadge(el));
  toast(T('Cleared local online-rate data'), 'success');
}
// proxyRateSet 控制台改数入口：proxyRateSet('香港-01', 0.42) 把该行最近在线率
// 改为 42% 并留存（m 标记 → 不再被后端覆盖）。match 按完整键 / 别名 / URL
// 片段匹配；无匹配且 match 非 URL 形态时新建一条仅本地展示的行（下拉不会
// 出现——下拉只列代理池里的 URL，但健康表会显示）。返回是否命中。
function proxyRateSet(match, rate) {
  const m = String(match || '').trim();
  if (!m || !(rate >= 0 && rate <= 1)) { console.warn('usage: proxyRateSet(aliasOrUrl, rate 0~1)'); return false; }
  const keys = Object.keys(proxyHealthByProxy);
  const k = keys.find(k => k === m) || keys.find(k => egLabel(k) === m) || keys.find(k => k.indexOf(m) >= 0)
    || (m.indexOf('://') < 0 ? 'mock://local#' + m : m);
  const row = Object.assign({}, proxyHealthByProxy[k] || { proxy: proxyHealthKey(k), attempts: 0, successes: 0 });
  const total = 40;
  row.recentTotal = total;
  row.recentOK = Math.round(rate * total);
  row.recentRate = rate;
  if (!row.attempts) { row.attempts = 150; row.successes = Math.round(150 * rate); }
  row.lastOk = new Date().toISOString();
  if (rate < 1 && !row.lastErr) row.lastErr = 'manual entry';
  row.m = 1;
  proxyHealthByProxy[k] = row;
  saveProxyHealthStore();
  renderProxyHealth(Object.values(proxyHealthByProxy));
  document.querySelectorAll('.eg-input').forEach(el => egSyncBadge(el));
  return true;
}
// 下拉行右侧展示的最近在线率：无样本返回 null（不标注）。底色按每 20%
// 一档共五档（见 proxyRateTier），替代旧"低率标红"的单档口径。
function proxyRecentRate(p) {
  const r = proxyHealthByProxy[proxyHealthKey(p)];
  if (!r || r.recentRate === null || r.recentRate === undefined) return null;
  return { text: Math.round(r.recentRate * 100) + '%', tier: proxyRateTier(r.recentRate) };
}
// proxyRecentHs 代理下拉/徽标用的平均握手延迟：数据与在线率同源
//（proxyHealthByProxy ← runtime.proxyHealth 的 avgHsMs），无样本或 0 返回
// null（不标注）。人类可读：1865ms→1.8s、653ms→0.6s（截断不四舍五入，
// 与在线率标签同款定宽徽标）。五档取在线率镜像（越低越好）：
// 0~2s 绿 / 2~4s 蓝 / 4~6s 粉 / 6~10s 黄 / ≥10s 红。
function proxyRecentHs(p) {
  const r = proxyHealthByProxy[proxyHealthKey(p)];
  const ms = Math.round(Number(r && r.avgHsMs) || 0);
  if (ms <= 0) return null;
  const text = ms < 100 ? '<0.1s' : (Math.floor(ms / 100) / 10) + 's';
  // 握手延迟五档（2026-10-18 用户定）：0~2s 绿 / 2~4s 蓝 / 4~6s 粉 / 6~10s 黄 /
  // ≥10s 红（红档无上限，覆盖 10~15s 及以上）。
  const tier = ms < 2000 ? 'green' : ms < 4000 ? 'blue' : ms < 6000 ? 'pink' : ms < 10000 ? 'yellow' : 'red';
  return { text: text, tier: tier };
}
// 在线率五档：≥80% 绿、≥60% 蓝、≥40% 粉、≥20% 黄、以下红；底色取主题色
//（双主题各一套）。代理下拉框（eg-badge/eg-rate）与代理池页健康表
//（pp-rate）共用同一口径与同一组颜色类。仅影响展示，不改路由行为。
function proxyRateTier(rate) {
  if (rate >= 0.8) return 'green';
  if (rate >= 0.6) return 'blue';
  if (rate >= 0.4) return 'pink';
  if (rate >= 0.2) return 'yellow';
  return 'red';
}

// ========== 账号表暂存编辑（三平台共用） ==========
// 代理绑定与"启用"勾选的改动只落在 DOM（行高亮 + 保存按钮描边），点击各
// 平台的"保存"按钮才逐行 diff 快照后提交后端，再刷新表格。清空代理/一键
// 启用/一键禁用同样只是暂存，保存后才生效。
const EDIT_SAVE_BTN = { acc: 'accSaveBtn', key: 'ocKeysSaveBtn', wb: 'wbSaveBtn', or: 'orKeysSaveBtn', amd: 'amdKeysSaveBtn', th: 'thKeysSaveBtn', other: 'otherKeysSaveBtn' };
function markRowDirty(row, kind) {
  if (row) row.classList.add('tr-dirty');
  const btn = _(EDIT_SAVE_BTN[kind] || 'accSaveBtn');
  if (btn) btn.classList.add('dirty-hint');
}
function clearDirtyMarks(tbodyId, btnId) {
  document.querySelectorAll('#' + tbodyId + ' tr.tr-dirty').forEach(tr => tr.classList.remove('tr-dirty'));
  const btn = _(btnId);
  if (btn) btn.classList.remove('dirty-hint');
}
// 清空单行代理绑定（暂存）：主=全局时备自动跟随锁定
function stageRowBindingClear(row, kind) {
  const main = row.querySelector('.eg-input[data-eg="' + kind + '"][data-slot="main"]');
  if (!main) return false;
  if (!main.dataset.value) return false;
  main.dataset.value = '';
  main.value = egLabel('');
  egSyncBadge(main);
  egApplyMainLinkage(row, '');
  return true;
}

// 各凭据页的表格容器（键 -> tbody 选择器），一键代理按页取行。
const BIND_TBODY = { acc: '#accountTableBody', trae: '#traeAccountsBody', key: '#ocKeysBody', wb: '#wbAccountsBody',
                    or: '#orKeysBody', amd: '#amdKeysBody', th: '#thKeysBody', other: '#otherKeysBody' };

// 一键代理：按行序把主代理依次绑定为 [直连, 代理池第1项, 代理池第2项, ...]。
// 仅暂存——与 Enable/Disable all、Clear bindings 同一口径，必须点 Save 才落盘。
// 不碰启用/禁用状态，也不主动写备用代理（备用槽只随既有的 egApplyMainLinkage 联动规则
// 跟随主槽；需要自定义备用代理请在开启备用代理后手动改）。
// 凭据多于代理时，多余的行保持原绑定不动。
function stageProxiesByOrder(kind) {
  const tbody = BIND_TBODY[kind];
  if (!tbody) return;
  const rows = [...document.querySelectorAll(tbody + ' tr')]
    .filter(r => r.querySelector('.eg-input[data-eg="' + kind + '"][data-slot="main"]'));
  if (!rows.length) { toast(T('No credentials to bind'), 'info'); return; }
  if (!proxyListCache.length) { toast(T('Proxy pool is empty'), 'info'); return; }
  // 自动分配规则：对该平台拉黑的代理不参与按序绑定（手动绑定不受限）。
  // Other 的规则按子平台存（other:<slug>），这里用当前选中的子平台取候选。
  const ruleKind = kind === 'other' && currentOtherSlug ? 'other:' + currentOtherSlug : kind;
  const pool = proxyListCache.filter(p => !proxyExcludedFor(p, ruleKind));
  const seq = proxyExcludedFor(EGRESS_DIRECT, ruleKind) ? pool : [EGRESS_DIRECT].concat(pool);
  let n = 0, left = 0;
  rows.forEach((row, i) => {
    if (i >= seq.length) { left++; return; }
    const main = row.querySelector('.eg-input[data-eg="' + kind + '"][data-slot="main"]');
    const v = seq[i];
    if (main.dataset.value === v) return;
    main.dataset.value = v;
    main.value = egLabel(v);
    egSyncBadge(main);
    egApplyMainLinkage(row, v);
    markRowDirty(row, kind);
    n++;
  });
  if (!n) { toast(T('Already bound in this order'), 'info'); return; }
  toast(T('Staged ') + n + T(' proxy binding(s) — click Save to apply') +
        (left ? T('; ') + left + T(' row(s) left unchanged (no proxy left)') : ''), 'success');
}

// ========== 凭据延迟重排（跨平台，弹窗选平台） ==========
// 点按钮先开确认弹窗（与导入配置同款式：文案 + 平台开关 + 全选/反选），确认后
// 把选中平台的凭据表中主槽绑定了真实代理的行（全局 ''/直连 direct 不参与），按
// 实测平均握手延迟重新分配代理：≤6s 快代理延迟升序在前 → 无记录代理（按池
// 序）→ >6s 慢代理（延迟升序）。重复绑定保留（同一代理出现几次进队列几次），
// 按各表行序从上到下依次回填。重选后逐平台自动调用既有保存函数落盘。
// 注意：懒加载 —— 各平台表格仅在访问过对应页后渲染，重排前先逐类加载；
// 自动保存会连带保存所选平台各表所有未落盘的暂存改动。
const REORDER_KINDS = ['acc', 'trae', 'wb', 'key', 'amd', 'or', 'th', 'other']; // 与左侧菜单同序
// 弹窗平台行（顺序 = REORDER_KINDS = 左侧菜单序）
const REORDER_PLATFORMS = [
  { key: 'acc', label: 'Cline' },
  { key: 'trae', label: 'TraeWork' },
  { key: 'wb', label: 'WorkBuddy' },
  { key: 'key', label: 'OpenCode' },
  { key: 'amd', label: 'AMDCloud' },
  { key: 'or', label: 'OpenRouter' },
  { key: 'th', label: 'TokenHarbor' },
  { key: 'other', label: 'Other' }
];
const REORDER_LOADERS = { acc: loadAccounts, trae: loadTraeConfig, key: loadOcConfig, wb: ensureWorkbuddyConsole,
                          or: loadOrConfig, amd: loadAmdConfig, th: loadTHConfig, other: loadOtherConfig };
const REORDER_SAVERS = { acc: saveAccountEdits, trae: saveTraeEdits, key: saveKeyEdits, wb: saveWorkbuddyEdits,
                         or: saveOrKeyEdits, amd: saveAmdKeyEdits, th: saveTHKeyEdits, other: saveOtherKeyEdits };
// 打开重排确认弹窗：平台行固定 7 项（与菜单同序），默认全部勾选
function openReorderDlg() {
  const grid = _('impReorderRows');
  grid.style.display = 'grid';
  grid.style.gridTemplateColumns = 'repeat(2, minmax(0,1fr))';
  grid.style.gridTemplateRows = 'repeat(' + Math.ceil(REORDER_PLATFORMS.length / 2) + ', auto)';
  grid.style.gridAutoFlow = 'column';
  grid.style.gap = '10px 18px';
  grid.innerHTML = REORDER_PLATFORMS.map(r =>
    '<label class="imp-keep-row"><span>' + esc(r.label) + '</span><span class="imp-tgl"><input type="checkbox" data-keep="' + r.key + '" checked><i></i></span></label>'
  ).join('');
  _('impReorderDialog').showModal();
}
function closeReorderDlg() { _('impReorderDialog').close(); }
function confirmReorder() {
  const kinds = [..._('impReorderRows').querySelectorAll('input[type=checkbox][data-keep]')]
    .filter(cb => cb.checked).map(cb => cb.getAttribute('data-keep'));
  _('impReorderDialog').close();
  if (!kinds.length) { toast(T('No platform selected'), 'info'); return; }
  reorderByLatency(kinds);
}
// 延迟数值（毫秒）：无记录返回 0。阈值 6000ms 与延迟徽标五档的 6s 分界（6~10s 黄）一致。
function proxyHsMsForReorder(v) {
  const r = proxyHealthByProxy[proxyHealthKey(v)];
  return Math.round(Number(r && r.avgHsMs) || 0);
}
function proxyPoolIdxForReorder(v) {
  const i = proxyListCache.indexOf(v);
  return i >= 0 ? i : Number.MAX_SAFE_INTEGER;
}
// 重排队列（三段式，行序从上到下依次取用）：
//   ① 已绑定的 ≤6s 快代理（延迟升序，重复绑定保留多份）
//   ② 无记录代理——已绑定的无记录 + **池内未绑定的无记录代理**（合并后按池序；
//      不在池内的失效绑定排同段末尾）
//   ③ 已绑定的 >6s 慢代理（延迟升序，重复绑定保留多份）
// 无记录段纳入池内未绑定代理，是让"无数据"节点排到慢节点之上、优先被启用的关键
//（否则从未绑定过的无记录代理永远不进队列，慢节点原位保留）。
// ===== 代理排除规则（原「自动分配规则」；平台级代理黑名单 + 每代理绑定上限）=====
// 数据源 /opencode/config 的 proxyExclusions（池条目 -> [kind...]）与
// proxyBindCaps（kind -> 上限，0/缺 = 不限），代理池页编辑。只约束一键代理
// 与延迟重排两条自动分配路径；手动绑定、配置导入与运行时路由不受影响。
let proxyExclusions = {};
let proxyBindCaps = {};
const PROXY_RULE_DIRECT_KEY = '__direct__';   // 直连在排除表里的哨兵键（与后端 proxyRuleDirectKey 一致）
// 直连不是池条目，映射到哨兵键后查同一张排除表
const proxyExcludedFor = (v, kind) => (proxyExclusions[v === EGRESS_DIRECT ? PROXY_RULE_DIRECT_KEY : v] || []).indexOf(kind) >= 0;
const proxyCapFor = kind => Math.max(0, Number(proxyBindCaps[kind]) || 0);
// 延迟档位：0=≤6s 快 1=无记录 2=>6s 慢（阈值与延迟徽标五档的 6s 分界一致）。
const proxyTierFor = v => { const ms = proxyHsMsForReorder(v); return ms > 6000 ? 2 : (ms > 0 ? 0 : 1); };

// 冻结-供给-修复回填（取代旧的三段式全量重排 —— 那会把快代理之间也互相洗牌）：
//   冻结段：绑定健康（快代理、未拉黑、未超上限）的行原样不动，绑定关系趋稳；
//   修复段：慢 / 无记录 / 被拉黑 / 超上限的行，从剩余供给按 快→无记录→慢 取最优：
//     · 被拉黑或超上限的行必须挪（哪怕只能拿到慢代理）；
//     · 其余行只在候选严格优于现状时才换 —— 慢不动慢、无记录不动无记录，
//       没有更优供给时保持原绑定，重排 diff 最小。
// 供给容量 = 上限 − 冻结占用 − 保留占用；被拉黑代理不进供给。每代理占用超过
// 上限的部分（第 cap+1 处起）按行序判为超限进入修复段。
// 返回实际改动行数。
function applyReorderRules(kind, parts) {
  const cap = proxyCapFor(kind);
  const inPool = v => proxyListCache.indexOf(v) >= 0;
  // 供给偏好序：池内未被拉黑的代理，快（延迟升序）→ 无记录（池序）→ 慢（延迟升序）
  const pref = proxyListCache.filter(p => !proxyExcludedFor(p, kind)).sort((a, b) => {
    const ta = proxyTierFor(a), tb = proxyTierFor(b);
    if (ta !== tb) return ta - tb;
    return ta === 1 ? proxyPoolIdxForReorder(a) - proxyPoolIdxForReorder(b)
                    : proxyHsMsForReorder(a) - proxyHsMsForReorder(b);
  });
  const cnt = {};          // 每代理已占用量（冻结 + 保留）
  const repair = [];
  const seen = {};
  parts.forEach(m => {
    const v = m.dataset.value;
    const ms = proxyHsMsForReorder(v);
    seen[v] = (seen[v] || 0) + 1;
    const overCap = cap > 0 && seen[v] > cap;
    const bad = proxyExcludedFor(v, kind) || overCap || ms > 6000 || ms <= 0;
    if (bad) repair.push({ m: m, v: v, forced: proxyExcludedFor(v, kind) || overCap });
    else cnt[v] = (cnt[v] || 0) + 1;
  });
  const capLeft = p => cap > 0 ? cap - (cnt[p] || 0) : Infinity;
  // 修复顺序：强制挪的优先（只能拿别人挑剩的），其余按行序
  repair.sort((a, b) => (b.forced ? 1 : 0) - (a.forced ? 1 : 0));
  let n = 0, stuck = 0;
  for (const r of repair) {
    let best = null;
    for (const p of pref) {
      if (p === r.v) continue;            // 候选 = 现状没有意义
      if (capLeft(p) > 0) { best = p; break; }
    }
    // 非强制行：没有候选或候选不严格更优 → 原样保留（保留也占容量）
    if (!best || (!r.forced && proxyTierFor(best) >= proxyTierFor(r.v))) {
      if (inPool(r.v)) cnt[r.v] = (cnt[r.v] || 0) + 1;
      if (!best) stuck++;
      continue;
    }
    cnt[best] = (cnt[best] || 0) + 1;
    if (best !== r.v) {
      r.m.dataset.value = best;
      r.m.value = egLabel(best);
      egSyncBadge(r.m);
      egApplyMainLinkage(r.m.closest('tr'), best);
      markRowDirty(r.m.closest('tr'), kind);
      n++;
    }
  }
  if (stuck) toast(T('Proxy exclusion rules: ') + stuck +
    T(' row(s) left unchanged — every allowed proxy is excluded or at cap'), 'warning', 6000);
  return n;
}
function collectReorderParts(kind) {
  const tbody = BIND_TBODY[kind];
  if (!tbody) return [];
  // 直连被该平台排除时，绑直连的行也进入修复段（强制挪到可用代理）；
  // 未排除时直连/全局照旧完全不参与重排（保持既有口径）。
  const directIn = proxyExcludedFor(EGRESS_DIRECT, kind);
  return [...document.querySelectorAll(tbody + ' tr')]
    .map(r => r.querySelector('.eg-input[data-eg="' + kind + '"][data-slot="main"]'))
    .filter(Boolean)
    .filter(m => m.dataset.value && (m.dataset.value !== EGRESS_DIRECT || directIn));
}
// Other 子平台逐个重排：Other 页一屏只渲染一个子平台的 keys（currentOtherSlug
// 选择器），saveOtherKeyEdits 也只按 slug 保存当前子平台 —— 必须逐个切换、
// 渲染、重排、保存，最后恢复用户原本选中的子平台。
async function reorderOtherAll() {
  const prevSlug = currentOtherSlug;
  const slugs = (otherConfig.platforms || []).map(p => p.slug);
  let total = 0;
  const failed = [];
  for (const slug of slugs) {
    currentOtherSlug = slug;
    _('otherPlatformSelect').value = slug;
    const p = (otherConfig.platforms || []).find(x => x.slug === slug);
    if (!p) continue;
    renderOtherKeys(p.keyStates || []);
    const parts = collectReorderParts('other');
    if (parts.length >= 2) total += applyReorderRules('other:' + slug, parts);
    try { await saveOtherKeyEdits(); } catch (e) { failed.push(slug); }
  }
  currentOtherSlug = prevSlug;
  _('otherPlatformSelect').value = prevSlug;
  await loadOtherConfig();
  return { total: total, failed: failed };
}
async function reorderByLatency(kinds) {
  const list = (kinds && kinds.length) ? REORDER_KINDS.filter(k => kinds.indexOf(k) >= 0) : REORDER_KINDS;
  if (!list.length) return;
  // 1. 懒加载兜底：逐类加载表格（失败的类跳过，最后汇总提示）
  const failedLoads = [];
  for (const k of list) {
    try { await REORDER_LOADERS[k](); } catch (e) { failedLoads.push(k); }
  }
  // 2. 每类表内独立重排；other 例外 —— 逐子平台处理（见 reorderOtherAll）
  let total = 0;
  const failedSaves = [];
  for (const kind of list) {
    if (failedLoads.indexOf(kind) >= 0) continue;
    if (kind === 'other') {
      const r = await reorderOtherAll();
      total += r.total;
      if (r.failed.length) failedSaves.push('other(' + r.failed.join('/') + ')');
      continue;
    }
    const parts = collectReorderParts(kind);
    if (parts.length < 2) continue; // 无绑定或仅一处，无从重排
    total += applyReorderRules(kind, parts);
  }
  // 3. 逐平台自动保存（other 已在步骤 2 内随子平台保存；其余各保存函数自带
  // diff 快照，仅提交真正变动的行）
  for (const k of list) {
    if (failedLoads.indexOf(k) >= 0 || k === 'other') continue;
    try { await REORDER_SAVERS[k](); } catch (e) { failedSaves.push(k); }
  }
  if (failedLoads.length || failedSaves.length) {
    toast(T('Latency reorder finished with errors — loads failed: ') + failedLoads.join(',') +
          T('; saves failed: ') + failedSaves.join(','), 'error');
    return;
  }
  if (!total) { toast(T('Already bound in this order'), 'info'); return; }
  toast(T('Reordered ') + total + T(' proxy binding(s) by latency and saved'), 'success');
}

let accSnapshot = {};
function stageAllAccountsPool(enabled) {
  document.querySelectorAll('#accountTableBody input[data-pool-enabled]').forEach(cb => {
    const snap = accSnapshot[cb.dataset.poolEnabled];
    if (snap && snap.enabled === enabled) return; // 与已保存状态一致，不产生待保存项
    cb.checked = enabled;
    markRowDirty(cb.closest('tr'), 'acc');
  });
}
function stageClearAccountProxies() {
  document.querySelectorAll('#accountTableBody tr').forEach(row => {
    const main = row.querySelector('.eg-input[data-eg="acc"][data-slot="main"]');
    if (!main) return;
    let dirty = stageRowBindingClear(row, 'acc');
    const cb = row.querySelector('input[data-pool-enabled]');
    if (cb && cb.checked) { cb.checked = false; dirty = true; }
    if (dirty) markRowDirty(row, 'acc');
  });
}
async function saveAccountEdits() {
  const rows = [...document.querySelectorAll('#accountTableBody tr')].filter(r => r.querySelector('.eg-input'));
  let changed = 0, failed = 0;
  for (const row of rows) {
    const mainInput = row.querySelector('.eg-input[data-eg="acc"][data-slot="main"]');
    const id = mainInput.dataset.egid;
    const main = mainInput.dataset.value || '';
    const backup = row.querySelector('.eg-input[data-eg="acc"][data-slot="backup"]').dataset.value || '';
    const cb = row.querySelector('input[data-pool-enabled]');
    const snap = accSnapshot[id] || { main: '', backup: '', enabled: true };
    try {
      if (main !== snap.main || backup !== snap.backup) {
        await api('POST', '/accounts/proxy', { accountId: id, main: main, backup: backup });
      }
      if (cb && cb.checked !== snap.enabled) {
        await api('POST', '/accounts/pool/enabled', { accountId: id, poolEnabled: cb.checked });
      }
      if (main !== snap.main || backup !== snap.backup || (cb && cb.checked !== snap.enabled)) changed++;
      row.classList.remove('tr-dirty');
    } catch (e) { failed++; }
  }
  clearDirtyMarks('accountTableBody', 'accSaveBtn');
  if (failed) toast(T('Save failed for ') + failed + T(' account(s)'), 'error');
  else if (!changed) toast(T('No changes to save'), 'info');
  else toast(T('Saved ') + changed + T(' account change(s)'), 'success');
  loadAccounts(); loadStats();
}

// 复位：丢弃全部暂存修改，按上次保存的状态重绘表格
function resetAccountEdits() {
  clearDirtyMarks('accountTableBody', 'accSaveBtn');
  loadAccounts();
}

// ========== 添加帐号弹窗（复用导入页的类型自动识别与后端接口） ==========
function openAccAddDialog() {
  _('accAddToken').value = '';
  _('accAddEmail').value = '';
  _('accAddDialog').showModal();
}
function closeAccAddDialog() { _('accAddDialog').close(); }
async function accAddFromDialog() {
  const token = _('accAddToken').value.trim();
  if (!token) { toast('Enter a token or API key', 'error'); return; }
  const email = _('accAddEmail').value.trim();
  const btn = _('accAddSubmit');
  btn.disabled = true;
  try {
    const d = await api('POST', '/accounts/add', { ...credPayload(token), email: email || undefined });
    toast(T('Account added: ') + (d.data.email || ''), 'success');
    closeAccAddDialog();
    loadAccounts(); loadStats();
  } catch (e) {
    toast(T('Add failed: ') + e.message, 'error');
  } finally {
    btn.disabled = false;
  }
}

// 批量测试：等价于对每个账号点一次 Test（真实探测，成功即复位冷却）。
// 逐个串行执行避免打爆上游；按钮上显示进度，结束后汇总一条 toast。
async function batchTestAccounts() {
  const btn = _('accTestBtn');
  const ids = Object.keys(accSnapshot);
  if (!ids.length) { toast(T('No accounts to test'), 'info'); return; }
  const original = btn ? btn.innerHTML : '';
  if (btn) btn.disabled = true;
  let ok = 0, cool = 0, err = 0;
  try {
    for (let i = 0; i < ids.length; i++) {
      if (btn) btn.innerHTML = '<span class="loading"></span>' + (i + 1) + '/' + ids.length;
      try {
        const d = await api('POST', '/accounts/test', { accountId: ids[i] });
        const r = d.data || {};
        if (r.status === 'active') ok++;
        else if (r.status === 'cooldown') cool++;
        else err++;
      } catch (e) { err++; }
    }
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = original; }
  }
  toast(T('Batch test done: ') + ids.length + T(' tested, ') + ok + T(' ok') +
    (cool ? T(', ') + cool + T(' cooling') : '') + (err ? T(', ') + err + T(' error') : ''),
    err ? 'warning' : 'success', 6000);
  loadAccounts(); loadStats();
}

function stageClearZenKeyProxies() {
  if (!confirm(T('Clear proxy bindings on ALL keys and disable routing? Click Save to apply.'))) return;
  document.querySelectorAll('#ocKeysBody tr').forEach(row => {
    const main = row.querySelector('.eg-input[data-eg="key"][data-slot="main"]');
    if (!main) return;
    let dirty = stageRowBindingClear(row, 'key');
    const cb = row.querySelector('input[data-zr]');
    if (cb && cb.checked) { cb.checked = false; dirty = true; }
    if (dirty) markRowDirty(row, 'key');
  });
}


// ========== 出口绑定联想下拉（combobox） ==========
// 选项固定为 全局 / 直连，其后是代理池按输入联想。联想阈值：有效字符 ≥3
//（中日韩字符按 1.5 计，即 2 个汉字触发）。value 语义：'' = 全局（默认，
// 隔离模式下沿用全局规则）；'direct' = 强制直连；代理 URL = 绑定出口。
const EGRESS_DIRECT = 'direct';
// 代理展示名：优先别名（URL 的 #fragment 随行持久化），无别名时打码凭据段。
// 绑定下拉、冷却状态等只显示别名，不暴露完整地址。
function egLabel(v) {
  if (!v) return T('Global');
  if (v === EGRESS_DIRECT) return T('Direct');
  try {
    const h = new URL(v).hash;
    if (h && h.length > 1) return decodeURIComponent(h.slice(1));
  } catch (e) { /* 非 URL 形态，回退打码 */ }
  return maskProxyLabel(v);
}
// egSearchText 代理的搜索匹配域(小写):别名 + 去协议前缀的原始串(host:port#alias)。
// 协议前缀(socks5:// 等)不含检索信息,纳进来会让 "e" 这类单字符命中全池——
// 四个搜索点(绑定联想框/排除卡片/卡片 Enter/批量追加)统一走本函数。
function egSearchText(p) {
  return (egLabel(p) + ' ' + p.replace(/^[a-z0-9]+:\/\//i, '')).toLowerCase();
}
// egBadgeHTML 代理值的闭合徽标 HTML：在线率 + 平均握手延迟，与下拉菜单项的
// eg-rate 同源（proxyHealthByProxy 缓存、同一套五档配色）。全局/直连或两个
// 指标都无样本时返回空串（不占位）。槽位恒定：在线率固定最右（right:6px），
// 握手延迟（.hs）恒定 right:46px——只有延迟时在线率位置空着，延迟不右移。
function egBadgeHTML(value) {
  if (!value || value === EGRESS_DIRECT) return '';
  const rate = proxyRecentRate(value);
  const hs = proxyRecentHs(value);
  if (!rate && !hs) return '';
  return (rate ? '<span class="eg-badge t-' + rate.tier + (rate.text.length > 3 ? ' w3' : '') + '">' + esc(rate.text) + '</span>' : '') +
    (hs ? '<span class="eg-badge hs w3 t-' + hs.tier + '">' + esc(hs.text) + '</span>' : '');
}
// egSyncBadge 按当前 dataset.value 重绘右侧在线率/握手延迟徽标（选中/失焦/
// 联动后调用）。双指标皆无或全局/直连时移除徽标并收回右侧留白。
function egSyncBadge(input) {
  const wrap = input.closest ? input.closest('.eg-wrap') : null;
  if (!wrap) return;
  const old = wrap.querySelectorAll('.eg-badge');
  old.forEach(el => el.remove());
  const html = egBadgeHTML(input.dataset.value || '');
  wrap.classList.toggle('has-badge', !!html);
  wrap.classList.toggle('has-hs', html.indexOf('eg-badge hs') >= 0);
  if (html) wrap.insertAdjacentHTML('beforeend', html);
}
function egComboHTML(kind, id, slot, value, title, disabled) {
  // 只读态的原因决定提示文案：备用代理开关关闭 ≠ 主槽是全局/直连
  const t = disabled
    ? T(slot === 'backup' && !backupProxyOn ? 'Backup proxies are disabled — follows the main slot' : 'Follows the main slot')
    : title;
  // 在线率徽标：选中后仍显示该节点的最近在线率与平均握手延迟，聚焦输入时由 CSS 隐藏
  const badge = egBadgeHTML(value || '');
  return '<span class="eg-wrap' + (badge ? ' has-badge' : '') + (badge.indexOf('eg-badge hs') >= 0 ? ' has-hs' : '') + '">' +
    '<input type="text" class="eg-input" data-eg="' + kind + '" data-egid="' + esc(id) + '" data-slot="' + slot + '" data-value="' + esc(value || '') + '" value="' + esc(egLabel(value)) + '" title="' + esc(t) + '"' + (disabled ? ' disabled' : '') + ' autocomplete="off" spellcheck="false">' +
    badge +
    '</span>';
}
// 主槽位联动：主 = 全局/直连 时备自动跟随相同值并只读；主 = 代理时备解锁。
// 备用代理开关关闭时备用槽无条件只读并跟随主槽 —— 关闭态下备用槽不参与出口选择，
// 留着可编辑只会让用户以为绑定生效了。
// 被锁定期间备的旧值会被跟随值覆盖（解锁后备回到"全局"，可重新选择）。
function egApplyMainLinkage(row, mainValue) {
  const backup = row ? row.querySelector('.eg-input[data-slot="backup"]') : null;
  if (!backup) return;
  if (!backupProxyOn || mainValue === '' || mainValue === EGRESS_DIRECT) {
    backup.dataset.value = mainValue;
    backup.value = egLabel(mainValue);
    egSyncBadge(backup);
    backup.disabled = true;
    backup.title = T(!backupProxyOn ? 'Backup proxies are disabled — follows the main slot' : 'Follows the main slot');
  } else {
    backup.disabled = false;
    backup.title = T('Backup exit — used when main is cooling/removed');
  }
}
function egCloseMenu() { const m = document.getElementById('egMenu'); if (m) m.remove(); }
function egOpenMenu(input) {
  egCloseMenu();
  const menu = document.createElement('div');
  menu.id = 'egMenu';
  menu.className = 'eg-menu';
  input.dataset.q = '';
  input.select();
  menu.addEventListener('mousedown', e => {
    const item = e.target.closest('.eg-item[data-value]');
    if (!item) return;
    e.preventDefault();
    egCommit(input, item.dataset.value);
  });
  document.body.appendChild(menu);
  egPositionMenu(input, menu);
  egRenderItems(input, menu, '');
}
function egPositionMenu(input, menu) {
  const r = input.getBoundingClientRect();
  menu.style.left = Math.min(r.left, window.innerWidth - 240) + window.scrollX + 'px';
  menu.style.top = r.bottom + window.scrollY + 4 + 'px';
  menu.style.minWidth = Math.max(r.width, 220) + 'px';
}
function egRenderItems(input, menu, q) {
  const cur = input.dataset.value || '';
  const kind = input.dataset.eg;
  const items = [
    { v: '', label: T('Global'), active: cur === '' },
    { v: EGRESS_DIRECT, label: T('Direct') + (kind && proxyExcludedFor(EGRESS_DIRECT, kind) ? T(' · excluded') : ''), active: cur === EGRESS_DIRECT }
  ];
  // 候选过滤:从第一个字符起即时过滤(与代理排除卡片输入框同一口径),
  // 空查询展示全部;别名或代理原始串任一包含即命中。无字符数门槛。
  const ql = q.toLowerCase();
  const matches = p => {
    return egSearchText(p).includes(ql);
  };
  const proxyItem = p => {
    const kind = input.dataset.eg;
    const ex = kind ? proxyExcludedFor(p, kind) : false;
    return { v: p, label: egLabel(p) + (ex ? T(' · excluded') : ''), active: cur === p,
             rate: proxyRecentRate(p), hs: proxyRecentHs(p) };
  };
  const proxies = q === ''
    ? proxyListCache.map(proxyItem)
    : proxyListCache.filter(matches).map(proxyItem);
  // 菜单项右侧双标签：在线率恒定最右，握手延迟（.eg-rate.hs-tag）恒定在其左
  //（right:46px）——只有延迟时在线率位置空着，延迟不右移；两者同 34px 定宽、
  // 同五档配色（延迟档位为其镜像）。
  let html = items.concat(proxies).map(it =>
    '<div class="eg-item' + (it.active ? ' active' : '') + ((it.rate || it.hs) ? ' has-rates' : '') + '" data-value="' + esc(it.v) + '">' + esc(it.label) +
    (it.rate ? '<span class="eg-rate t-' + it.rate.tier + (it.rate.text.length > 3 ? ' w3' : '') + '">' + it.rate.text + '</span>' : '') +
    (it.hs ? '<span class="eg-rate hs-tag w3 t-' + it.hs.tier + '">' + it.hs.text + '</span>' : '') +
    '</div>'
  ).join('');
  if (q !== '' && !proxies.length) html += '<div class="eg-item eg-hint">' + T('No matching proxies') + '</div>';
  menu.innerHTML = html;
}
function egCommit(input, value) {
  input.dataset.value = value;
  input.value = egLabel(value);
  egSyncBadge(input);
  egCloseMenu();
  input.blur();
  if (input.dataset.slot === 'main') egApplyMainLinkage(input.closest('tr'), value);
  // 暂存模型：绑定改动只落 DOM 并高亮行，由各平台"保存"按钮统一提交
  markRowDirty(input.closest('tr'), input.dataset.eg);
}
document.addEventListener('focusin', e => {
  if (e.target.classList && e.target.classList.contains('eg-input')) egOpenMenu(e.target);
});
document.addEventListener('input', e => {
  if (!e.target.classList || !e.target.classList.contains('eg-input')) return;
  const menu = document.getElementById('egMenu');
  if (menu) egRenderItems(e.target, menu, e.target.value);
  else egOpenMenu(e.target);
});
document.addEventListener('keydown', e => { if (e.key === 'Escape') egCloseMenu(); });
document.addEventListener('blur', e => {
  if (e.target.classList && e.target.classList.contains('eg-input')) {
    // 未选择直接失焦：显示还原为当前绑定值的标签
    e.target.value = egLabel(e.target.dataset.value);
    egSyncBadge(e.target);
    setTimeout(egCloseMenu, 150);
  }
}, true);

async function testAccount(id, btn) {
  const original = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span>Testing'; }
  try {
    const d = await api('POST', '/accounts/test', { accountId: id });
    const r = d.data || {};
    const statusMap = { active: 'Available', cooldown: 'Cooldown', expired: 'Expired', error: 'Error' };
    const label = statusMap[r.status] || r.status;
    const prevMap = { active: 'Active', cooldown: 'Cooldown', expired: 'Expired', '': '' };
    // toast 用 textContent 渲染，参数一律不要再 esc()：转义会以字面量显示
    //（429 的错误体就变成可见的 &quot;error&quot;:…）
    let msg = T('Account ') + (r.email || '') + T(' — ') + label;
    if (r.prevStatus && r.prevStatus !== r.status) msg += T(' (was: ') + (prevMap[r.prevStatus] || r.prevStatus) + T(')');
    if (r.cooldownUntil) msg += T('\nEstimated recovery: ') + fmtWhen(r.cooldownUntil);
    if (r.remaining) msg += T(' (remaining ') + r.remaining + T(')');
    // 成功时不显示"原因 ok / HTTP 200"这类噪音：只有非成功结果才需要原因
    if (r.status !== 'active') {
      if (r.reason) msg += T('\nReason: ') + r.reason;
      if (r.httpStatus) msg += T('\nHTTP: ') + r.httpStatus;
    }
    const type = r.status === 'active' ? 'success' : (r.status === 'cooldown' ? 'warning' : 'error');
    toast(msg, type, 6000);
    loadAccounts(); loadStats();
  } catch (e) {
    toast(T('Test failed: ') + e.message, 'error');
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = original; }
  }
}

async function deleteAccount(id) {
  if (!confirm(T('Delete this account?'))) return;
  try {
    await api('POST', '/accounts/delete', { accountId: id });
    toast('Account deleted', 'success');
    loadAccounts(); loadStats();
  } catch (e) { toast(T('Delete failed: ') + e.message, 'error'); }
}

async function resetAccount(id, btn) {
  const original = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span>Checking'; }
  try {
    const d = await api('POST', '/accounts/reset', { accountId: id });
    const r = d.data || {};
    const type = d.success ? 'success' : (r.status === 'cooldown' ? 'warning' : 'error');
    let msg = d.message || T('Check complete');
    // 恢复时刻与剩余时长都由面板渲染：服务器发的是 RFC3339 时刻（浏览器本地
    // 时区显示），remaining 只在这里加一次，避免与服务器消息重复。
    // toast 走 textContent，不要再 esc()（会显示成字面量 &quot;）
    if (r.cooldownUntil && r.status === 'cooldown') msg += T(' (estimated recovery ') + fmtWhen(r.cooldownUntil) + T(')');
    if (r.remaining && r.status !== 'active') msg += T(' (remaining ') + r.remaining + T(')');
    toast(msg, type, 6000);
    loadAccounts(); loadStats();
  } catch (e) {
    toast(T('Check failed: ') + e.message, 'error');
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = original; }
  }
}

// ========== Config export / import ==========

async function exportConfig() {
  try {
    const res = await fetch(API + '/config/export');
    if (!res.ok) throw new Error('HTTP ' + res.status);
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url; a.download = 'afree-proxy-config-' + new Date().toISOString().slice(0, 10) + '.json';
    document.body.appendChild(a); a.click(); document.body.removeChild(a);
    URL.revokeObjectURL(url);
    toast(T('Config exported'), 'success');
  } catch (e) { toast(T('Export failed: ') + e.message, 'error'); }
}

let importParsed = null; // 已通过版本校验、待弹窗确认的备份 JSON

function onImportFileChange(event) {
  const file = event.target.files[0];
  if (!file) return;
  const reader = new FileReader();
  reader.onload = () => {
    try {
      const parsed = JSON.parse(reader.result);
      if (parsed.version !== 1 && parsed.version !== 2) throw new Error(T('Unsupported backup version'));
      const rows = impKeepRowsFor(parsed);
      if (rows.length === 0) {
        // 备份不含任何上游身份：无沿用选项，按原确认流程直接导入
        if (!confirm(T('Import replaces sections present in the file (accounts / keys / proxies / combos ...). All imported identities start DISABLED. Continue?'))) return;
        doImportConfig(parsed, {});
        return;
      }
      importParsed = parsed;
      // 两列网格、列优先填充（从上到下再从左到右）：行数按 ceil(n/2) 动态生成，
      // grid-auto-flow:column 先填满第一列再填第二列；高度随内容自适应。
      const grid = _('impKeepRows');
      grid.style.display = 'grid';
      grid.style.gridTemplateColumns = 'repeat(2, minmax(0,1fr))';
      grid.style.gridTemplateRows = 'repeat(' + Math.ceil(rows.length / 2) + ', auto)';
      grid.style.gridAutoFlow = 'column';
      grid.style.gap = '10px 18px';
      grid.style.marginBottom = '14px';
      grid.innerHTML = rows.map(r =>
        '<label class="imp-keep-row"><span>' + esc(r.label) + '</span><span class="imp-tgl"><input type="checkbox" data-keep="' + esc(r.key) + '"><i></i></span></label>'
      ).join('');
      _('impKeepDialog').showModal();
    } catch (e) {
      importParsed = null;
      toast(T('Invalid backup file: ') + e.message, 'error');
    }
  };
  reader.readAsText(file);
  event.target.value = '';
}

// impKeepRowsFor 从备份 JSON 提取含上游身份的平台行（Other 子平台逐行，
// key 用 "other:<slug>" 与后端 KeepEnabled 对齐）。行序与左侧菜单一致：
// Cline → OpenCode → WorkBuddy → AMDCloud → OpenRouter → TokenHarbor → Other。
// 全不勾选 = 原隔离护栏行为。
function impKeepRowsFor(parsed) {
  const rows = [];
  const has = a => Array.isArray(a) && a.length > 0;
  if (has(parsed.accounts)) rows.push({ key: 'cline', label: 'Cline' });
  if (has(parsed.zen && parsed.zen.keys)) rows.push({ key: 'zen', label: 'OpenCode' });
  if (parsed.workbuddy && parsed.workbuddy.authFiles && Object.keys(parsed.workbuddy.authFiles).length > 0) rows.push({ key: 'wb', label: 'WorkBuddy' });
  if (has(parsed.amdcloud && parsed.amdcloud.keys)) rows.push({ key: 'amd', label: 'AMDCloud' });
  if (has(parsed.openrouter && parsed.openrouter.keys)) rows.push({ key: 'oprt', label: 'OpenRouter' });
  if (has(parsed.tokenharbor && parsed.tokenharbor.keys)) rows.push({ key: 'tkhb', label: 'TokenHarbor' });
  (parsed.other && Array.isArray(parsed.other.platforms) ? parsed.other.platforms : []).forEach(p => {
    if (p && p.slug && has(p.keys)) rows.push({ key: 'other:' + p.slug, label: p.name || p.slug });
  });
  return rows;
}

function closeImpKeep() { _('impKeepDialog').close(); importParsed = null; }

// impKeepSetAll 全选/反选导入/重排弹窗的平台开关：invert=false 全部勾上，true 逐个取反。
function impKeepSetAll(gridId, invert) {
  _(gridId).querySelectorAll('input[type=checkbox][data-keep]').forEach(cb => {
    cb.checked = invert ? !cb.checked : true;
  });
}

function confirmImpKeep() {
  if (!importParsed) return;
  const keep = {};
  _('impKeepRows').querySelectorAll('input[type=checkbox][data-keep]').forEach(cb => {
    if (cb.checked) keep[cb.getAttribute('data-keep')] = true;
  });
  const parsed = importParsed;
  importParsed = null;
  _('impKeepDialog').close();
  doImportConfig(parsed, keep);
}

async function doImportConfig(parsed, keep) {
  const payload = Object.assign({}, parsed, { keepEnabled: keep });
  try {
    const d = await api('POST', '/config/import', payload);
    const imp = (d.data && d.data.imported) || {};
    const parts = Object.keys(imp).map(k => k + '=' + imp[k]);
    toast(T('Config imported') + ' — ' + parts.join(', '), 'success');
    setTimeout(() => location.reload(), 1200);
  } catch (e) { toast(T('Import failed: ') + e.message, 'error'); }
}


// ========== Token import ==========
// Credential type auto-detection: "sk_..." = static Cline API key, anything
// else = OAuth refreshToken (the backend validates refresh tokens upstream;
// API keys are pooled as-is).
const credPayload = t => t.startsWith('sk_') ? { apiToken: t } : { refreshToken: t };

// ========== API keys ==========
async function loadKeys() {
  try {
    const d = await api('GET', '/keys');
    const keys = d.data.keys;
    const el = _('keysList');
    if (!keys || keys.length === 0) {
      el.innerHTML = '<div class="empty-state"><span class="icon"><svg viewBox="0 0 24 24" width="36" height="36" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21 2l-2 2m-7.6 7.6a5.5 5.5 0 1 1-7.78 7.78 5.5 5.5 0 0 1 7.78-7.78zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4"/></svg></span>No API keys yet</div>';
      return;
    }
    el.innerHTML = keys.map(k =>
      '<div class="flex" style="margin-bottom:8px">' +
        '<span class="key-display" style="flex:1" data-copy="' + esc(k) + '" title="Click to copy">' + esc(k) + '</span>' +
        '<button class="btn btn-sm btn-danger" data-delkey="' + esc(k) + '">Delete</button>' +
      '</div>'
    ).join('');
    el.onclick = e => {
      const c = e.target.closest('[data-copy]');
      if (c) { copyText(c.dataset.copy); return; }
      const d = e.target.closest('button[data-delkey]');
      if (d) deleteKey(d.dataset.delkey);
    };
  } catch (e) { _('keysList').innerHTML = '<div class="empty">Failed to load</div>'; }
}

async function generateKey() {
  try {
    const d = await api('POST', '/keys/generate');
    const key = d.data.key;
    _('keyGenResult').innerHTML =
      '<div style="background:rgba(52,211,153,.08);border:1px solid rgba(52,211,153,.4);border-radius:10px;padding:12px">' +
        '<div style="color:var(--accent2);font-weight:600;margin-bottom:8px">New key generated (click to copy)</div>' +
        '<div class="key-display" data-copy="' + esc(key) + '">' + esc(key) + '</div>' +
      '</div>';
    const kr = _('keyGenResult').querySelector('[data-copy]');
    if (kr) kr.onclick = () => copyText(kr.dataset.copy);
    loadKeys();
    toast('Key generated', 'success');
    setTimeout(() => _('keyGenResult').innerHTML = '', 8000);
  } catch (e) { toast(T('Generation failed: ') + e.message, 'error'); }
}

async function deleteKey(key) {
  if (!confirm(T('Delete this key?'))) return;
  try {
    await api('POST', '/keys/delete', { key });
    toast('Key deleted', 'success');
    loadKeys();
  } catch (e) { toast(T('Delete failed: ') + e.message, 'error'); }
}


async function deleteAllData() {
  if (!confirm(T('Delete ALL persisted data? This will clear Cline accounts and client keys, OpenCode keys, WorkBuddy accounts, the proxy pool, default model, request headers, scheduling strategy, custom aliases, sticky sessions, and request logs. This cannot be undone!'))) return;
  try {
    await api('POST', '/data/delete-all', {});
    toast(T('All persisted data deleted'), 'success');
    setTimeout(() => location.reload(), 1000);
  } catch (e) { toast(T('Delete failed: ') + e.message, 'error'); }
}

function copyText(t, okMsg, fallbackEl) {
  const done = () => toast(okMsg || 'Copied to clipboard', 'success');
  const fail = () => {
    // 复制被浏览器策略挡下时必须如实报错，不能谎报成功——之前无条件弹
    // 「已复制」，用户粘贴时才发现剪贴板是空的。选中来源输入框方便手动 Ctrl+C。
    if (fallbackEl) { fallbackEl.focus(); fallbackEl.select(); }
    toast(T('Copy failed'), 'error');
  };
  // clipboard API 只在 secure context（https/localhost）可用；navigator.clipboard 为
  // undefined 时直接调用会同步抛错，.catch 接不住，必须先判空再走 execCommand 降级。
  if (navigator.clipboard && window.isSecureContext) {
    navigator.clipboard.writeText(t).then(done, () => { if (legacyCopy(t)) done(); else fail(); });
    return;
  }
  if (legacyCopy(t)) done(); else fail();
}
function legacyCopy(t) {
  const ta = document.createElement('textarea');
  ta.value = t;
  // 模态 <dialog> 打开时其背景元素全部 inert：挂到 body 的 textarea 拿不到焦点，
  // select() 选区为空，execCommand('copy') 静默失败（实测返回 false）——添加账号
  // 弹窗里的 Copy 按钮正是这种情况。挂到焦点所在的 dialog 内即可绕开；无弹窗时
  // 退回 body（key 列表等页面内复制不受影响）。
  const host = document.activeElement && document.activeElement.closest('dialog');
  (host || document.body).appendChild(ta);
  ta.focus(); ta.select();
  const ok = document.execCommand('copy');
  ta.remove();
  return ok;
}

// ========== Request logs ==========
// 路由名与左侧菜单同名、全小写（见 nav-item data-tab）：cline / opencode /
// workbuddy / amdcloud / openrouter / tokenharbor / other。Other 的各子平台
// 日志展示其自定义前缀（后端 logs.go 直接写入前缀，此表兜底不命中）。
// admin / meta / unclassified 非平台路由，不在菜单中。
const ROUTE_LABEL = { zen: 'opencode', cline: 'cline', admin: 'admin', meta: 'meta', workbuddy: 'workbuddy', openrouter: 'openrouter', amd: 'amdcloud', tokenharbor: 'tokenharbor', other: 'other', unclassified: 'unclassified' };
const PROXY_LABEL = { main: 'Main', backup: 'Backup', direct: 'Direct' };
const STATUS_CLASS = s => s >= 400 ? 'color:var(--danger)' : 'color:var(--accent2)';
let logsAuto = true;

function toggleLogsAuto() {
  logsAuto = !logsAuto;
  _('logsAutoBtn').textContent = logsAuto ? 'Pause' : 'Resume';
  const pill = _('logsAutoPill');
  pill.textContent = logsAuto ? 'Auto-refreshing' : 'Paused';
  pill.classList.toggle('paused', !logsAuto);
  if (logsAuto) loadLogs();
}

async function loadLogs() {
  const tbody = _('logsTableBody');
  try {
    const d = await api('GET', '/logs');
    const logs = d.data.logs || [];
    if (!logs.length) { tbody.innerHTML = '<tr><td colspan="11" class="empty">No requests logged</td></tr>'; return; }
    tbody.innerHTML = logs.map(l => {
      const t = l.time ? new Date(l.time).toLocaleString('en-US') : '-';
      const route = ROUTE_LABEL[l.route] || l.route || '-';
      const st = l.status || 0;
      const proxy = PROXY_LABEL[l.proxyType] || '-';
      return '<tr>' +
        '<td class="mono" style="font-size:11px">' + t + '</td>' +
        '<td class="mono" style="font-size:11px">' + esc(l.client || '-') + '</td>' +
        '<td>' + esc(l.method || '-') + '</td>' +
        '<td class="mono" style="font-size:11px">' + esc(l.path || '-') + '</td>' +
        '<td class="mono" style="font-size:12px">' + esc(l.model || '-') + '</td>' +
        '<td><span class="model-tag">' + esc(route) + '</span></td>' +
        '<td class="mono" style="font-size:11px">' + esc(l.upstream || '-') + '</td>' +
        '<td>' + esc(proxy) + '</td>' +
        '<td style="font-weight:600;' + STATUS_CLASS(st) + '"' + (l.error ? ' title="' + esc(l.error) + '"' : '') + '>' + st + '</td>' +
        '<td class="mono" style="font-size:11px" title="' + esc(T('Time to first byte: from request start to the first response byte (streaming = first upstream token; - = no response body, e.g. 413)')) + '">' + (l.ttfb_ms == null || l.ttfb_ms < 0 ? '-' : l.ttfb_ms + ' ms') + '</td>' +
        '<td class="mono" style="font-size:11px">' + (l.duration_ms != null ? l.duration_ms + ' ms' : '-') + '</td>' +
      '</tr>';
    }).join('');
  } catch (e) { tbody.innerHTML = '<tr><td colspan="11" class="empty">Failed to load</td></tr>'; }
}

// ========== Config updates ==========
// saveAdminLogToggle 勾选即存：/admin 页面与接口访问是否写入请求日志。
// 关闭（默认）只影响面板自身产生的行，经过网关的模型会话请求日志不受影响。
async function saveAdminLogToggle() {
  const cb = _('settingAdminLog');
  try {
    await api('POST', '/config/update', { adminLogEnabled: cb.checked });
    toast(cb.checked ? 'Admin access logging enabled' : 'Admin access logging disabled', 'success');
  } catch (e) {
    cb.checked = !cb.checked;  // 保存失败回滚勾选，避免界面与落盘状态不一致
    toast(T('Save failed: ') + e.message, 'error');
  }
}

function addHeaderRow() {
  const tbody = _('headersTableBody');
  const tr = document.createElement('tr');
  tr.innerHTML =
    '<td><input type="text" class="header-key" placeholder="Header-Name" style="font-size:12px;font-family:monospace"></td>' +
    '<td><input type="text" class="header-val" placeholder="value" style="font-size:12px;font-family:monospace"></td>' +
    '<td><button class="btn btn-sm btn-danger" onclick="this.closest(\'tr\').remove()">Delete</button></td>';
  tbody.appendChild(tr);
}

async function resetHeaders() {
  try {
    await api('POST', '/headers/reset', {});
    toast(T('Headers restored to defaults'), 'success');
    loadConfig();
  } catch (e) { toast(T('Reset failed: ') + e.message, 'error'); }
}

async function saveHeaders() {
  const tbody = _('headersTableBody');
  const rows = tbody.querySelectorAll('tr');
  const headers = {};
  let hasEmpty = false;
  rows.forEach(tr => {
    const keyInput = tr.querySelector('.header-key');
    const valInput = tr.querySelector('.header-val');
    if (keyInput && valInput) {
      const k = keyInput.value.trim();
      const v = valInput.value.trim();
      if (k) { headers[k] = v; }
      else if (v) { hasEmpty = true; }
    }
  });
  if (hasEmpty) { toast('Rows with a value but no key were ignored', 'info'); }
  try {
    const d = await api('POST', '/config/update', { headers });
    toast('Headers saved', 'success');
    loadConfig();
  } catch (e) { toast(T('Save failed: ') + e.message, 'error'); }
}

const WB_RATE_OPTIONS = [0, 0.1, 0.2, 0.3, 0.5, 1.0];
// 倍率筛选阈值：服务端配置（wbconfig pool.model_rate_filter）持有，网关
// /v1/models 与面板同口径过滤。初值 0.2 只是首屏未加载完的占位——loadModels /
// loadWorkbuddyConfig 拉到真值后立刻覆盖，落盘由 setWorkbuddyRateFilter 负责。
let workbuddyRateFilter = 0.2;
// 倍率解析：null / undefined / 空串 / 不可解析 → null（= 缺失），其余为数值。
// 不能用 String(value ?? '').replace(...) 一把梭：那个式子把 null/'' 洗成
// Number('') = 0，等于宣称「缺失 = 免费」，与 matchesModelRateFilter 的
// server 口径（raw == "" 或 ParseFloat 失败即 ok=false → 隐藏）相反——试用
// 模型显式清空倍率时会把付费模型当成 0 放进池子。
function wbNumber(value) {
  if (value === null || value === undefined) return null;
  // 空串/纯空白：上游"没给倍率"的常见形态（含显式清空的试用模型）
  if (typeof value === 'string' && value.trim() === '') return null;
  // 剥掉 'x0.2' 这类装饰后必须还剩数字：否则 'abc'/'NaN'/'Infinity' 会被清成
  // '' 再走成 Number('') = 0，等于把"不可解析"谎报成免费（server 侧
  // ParseFloat 失败即 ok=false）。
  const cleaned = String(value).replace(/[^\d.-]/g, '');
  if (!/\d/.test(cleaned)) return null;
  const n = Number(cleaned);
  return Number.isFinite(n) ? n : null;
}
function wbRate(m) {
  const promoted = m && m.promo_factor !== undefined && m.promo_factor !== null &&
    m.promo_credits !== undefined && m.promo_credits !== null && m.promo_credits !== '';
  return wbNumber(promoted ? m.promo_credits : m && m.credits);
}
// 倍率缺失（null）的模型一律隐藏：缺失 ≠ 免费（试用模型显式清空倍率、目录
// 未刷新时整表缺失、上游没给 credits，都可能是收费模型），无法证明 ≤ 阈值就
// 不展示，与服务端 matchesModelRateFilter 同口径。
function wbModelMatchesFilter(m) {
  const rate = wbRate(m);
  return rate !== null && rate <= workbuddyRateFilter + 1e-9;
}
// WorkBuddy vendor realm 前缀 → 网关前缀（与后端 wbVendorToRealm 同构）。
// vendor /models 直接给 cn:/global:，但面板展示的模型 ID 必须与 /v1/models
// 里发布的（wbcn:/wbgb:）一致，否则默认模型/自定义别名存下的是 vendor 前缀，
// 而 validateCombo 用网关形态的 modelList 校验 → combo 永远校验不过。
const WB_REALM_PREFIXES = [['wbcn:', 'cn:'], ['wbgb:', 'global:']];
function wbRealmToGatewayId(id) {
  const s = String(id == null ? '' : id);
  for (const [gw, vendor] of WB_REALM_PREFIXES) {
    if (s.startsWith(vendor)) return gw + s.slice(vendor.length);
  }
  return s;
}
async function fetchWorkbuddyModels() {
  try {
    const data = await wbCall('models');
    // 副本而非原地改：同一批对象还要供倍率筛选与 context 渲染，且 wbCall
    // 的返回值可能被其他调用方复用。
    return (data.models || []).map(m => {
      if (!m || typeof m.id !== 'string') return m;
      return Object.assign({}, m, { id: wbRealmToGatewayId(m.id) });
    });
  } catch (e) {
    return [];
  }
}
function wbContextLabel(value) {
  const n = Number(value || 0) / 1000;
  if (!(n > 0)) return '—';
  const tiers = [[32, '32K'], [64, '64K'], [128, '128K'], [256, '256K'], [512, '512K'], [1024, '1M'], [2048, '2M']];
  let best = tiers[0];
  tiers.forEach(t => {
    if (Math.abs(n - t[0]) <= Math.abs(n - best[0])) best = t;
  });
  return best[1];
}
function wbContextTagHTML(value, title) {
  const large = Number(value || 0) >= 1000000;
  return '<span class="model-tag' + (large ? ' context-1m' : '') + '" title="' + esc(title) + '">' + esc(wbContextLabel(value)) + '</span>';
}
function wbRateHTML(m) {
  const rate = wbRate(m);
  const base = wbNumber(m.credits);
  const value = rate === null ? '—' : 'x' + rate;
  const old = base !== null && rate !== null && base !== rate ? ' <s style="color:var(--text3)">x' + esc(base) + '</s>' : '';
  const promo = m.promo_label ? ' <span class="model-tag">' + esc(m.promo_label) + '</span>' : '';
  return '<span title="' + esc(m.promo_note || '') + '">' + value + old + promo + '</span>';
}
// setWorkbuddyRateFilter 已随目录区内嵌的倍率筛选下拉移除（2026-10-24 UI 统一）：
// 倍率改在 WorkBuddy configuration 表单的 Model rate filter 调整，保存后经
// saveWorkbuddyConfig → loadModels 热刷新目录，无需独立入口。

async function loadModels() {
  try {
    // 倍率阈值先于渲染拉取：它决定 wb 名单与下拉选中项（服务端配置，
    // wbconfig pool.model_rate_filter）。
    await syncWorkbuddyRateFilter();
    // 黑名单同样先于渲染拉取：各页模型块都要剔除已封禁模型（封禁模型区的
    // 语义是「不再对外提供」，设置页列表不能还展示它们）。
    await syncModelBlacklist();
    const d = await api('GET', '/models');
    const models = d.data.models || [];
    const clineCredOk = d.data.credentialsEnabled !== false;
    let info = '';
    if (d.data.lastSync) info += T('· official feed: ') + new Date(d.data.lastSync).toLocaleTimeString('en-US');
    // 目录区已分平台迁回各自页面：同名 pill（data-models-info）每页一个，统一回填。
    document.querySelectorAll('[data-models-info]').forEach(el => { el.textContent = info; });
    const row = (inner) => '<div style="display:flex;align-items:center;gap:10px;padding:8px 12px;margin:5px 0;background:rgba(148,163,184,.06);border:1px solid var(--border);border-radius:10px;transition:.15s">' + inner + '</div>';
    // 黑名单过滤：cline/zen 目录返回的是裸 ID（对外形态 zen: 前缀由网关拼），
    // 所以两种形态都查；wb 目录返回的已带 wbcn:/wbgb: 前缀。
    const notBlocked = m => m && !modelBlacklistIds.has(m.id) && !modelBlacklistIds.has('zen:' + m.id);
    // 封禁按钮（手动封禁 = 加入黑名单，原因「管理员手动封禁」）。
    const banBtn = (platform, model, refresh) =>
      '<button class="btn btn-sm btn-danger" data-bl-ban="1" data-platform="' + esc(platform) + '" data-model="' + esc(model) + '"' +
      (refresh ? ' data-refresh="' + esc(refresh) + '"' : '') + ' style="white-space:nowrap">' + T('Ban') + '</button>';
    // Cline 免费模型（账号页底部目录区）
    const accBox = _('accModelsBody');
    if (accBox) {
      const clineV = models.filter(notBlocked);
      accBox.innerHTML = clineV.length
        ? clineV.map(m => row(
            '<span style="font-family:\'JetBrains Mono\',monospace;font-size:13px;flex:1">' + esc(m.id) + '</span>' +
            '<span class="model-tag free">' + T('no charge') + '</span>' +
            banBtn('cline', m.id))).join('')
        : '<div class="empty">' + T(clineCredOk ? 'No models' : 'Models appear here once an account is enabled — the catalog syncs from upstream') + '</div>';
    }
    // OpenCode 免费模型（OpenCode 页底部目录区）
    const ocBox = _('ocModelsBody');
    if (ocBox) {
      const zr = await api('GET', '/opencode/models').then(r => r.data).catch(() => null);
      const zen = (zr && zr.models) || [];
      const zenCredOk = !zr || zr.credentialsEnabled !== false;
      const zenV = zen.filter(notBlocked);
      ocBox.innerHTML = zenV.length
        ? zenV.map(m => row(
              '<span style="font-family:\'JetBrains Mono\',monospace;font-size:13px;flex:1">' + esc(m.id) + '</span>' +
              '<span class="model-tag free">' + T('no charge') + '</span>' +
              wbContextTagHTML(m.context, T('Context')) +
              // 目录是裸 ID，对外形态带 zen: 前缀——封禁按对外 ID 记。
              banBtn('zen', 'zen:' + m.id))).join('')
        : '<div class="empty">' + T(zenCredOk ? 'No models' : 'Models appear here once a credential is enabled — the catalog syncs from upstream') + '</div>';
    }
    // WorkBuddy 模型（WorkBuddy 页底部目录区；行样式与 cline/OpenCode 目录
    // 完全一致，不再内嵌倍率筛选行——倍率走上方配置表单的 Model rate filter，
    // 保存后经 saveWorkbuddyConfig → loadModels 热刷新目录）
    const wbBox = _('wbModelsBody');
    if (wbBox) {
      // 空态分情境（与 TraeWork/AMDCloud 统一口径）：上游目录空（无启用账号
      // 时后端 503，fetch 返回 []）→ 引导启用账号；目录有但被筛选/封禁光 →
      // 保留筛选提示。
      let wbAll = [];
      let wbUnavailable = false;
      try { wbAll = await fetchWorkbuddyModels(); } catch (e) { wbUnavailable = true; }
      const wbModels = wbAll.filter(notBlocked).filter(wbModelMatchesFilter).sort((a, b) => wbRate(a) - wbRate(b));
      const wbBlock = wbModels.length
        ? wbModels.map(m => {
            const rate = wbRate(m);
            const free = rate === 0 ? '<span class="model-tag free">' + T('free') + '</span>' : '';
            return row(
              '<span style="font-family:\'JetBrains Mono\',monospace;font-size:13px;flex:1" title="' + esc(m.description || '') + '">' + esc(m.id) + '</span>' +
              free +
              '<span class="model-tag">' + wbRateHTML(m) + '</span>' +
              '<span class="model-tag" title="' + T('Default tier') + '">' + esc(m.default_effort || '—') + '</span>' +
              wbContextTagHTML(m.context_length, T('Maximum context length')) +
              banBtn('workbuddy', m.id));
          }).join('')
        : '<div class="empty">' + T((wbUnavailable || wbAll.length === 0)
            ? 'Models appear here once an account is enabled — the catalog syncs from upstream'
            : 'No WorkBuddy models under the current filter') + '</div>';
      wbBox.innerHTML = wbBlock;
    }
    loadModelBlacklist();
  } catch (e) {
    ['accModelsBody', 'ocModelsBody', 'wbModelsBody'].forEach(id => {
      const el = _(id);
      if (el) el.textContent = T('Failed to load');
    });
  }
}

// ========== Banned models（封禁模型） ==========
// 自动封禁：上游按账号类型拒掉的模型（code 11158 等）在网关出口命中判据后进这张表。
// 手动封禁：各模型展示区的「封禁」按钮。两类均已从 /v1/models 移除。
// Test = 用该平台第一个启用凭据发一次真实请求；Unblock = 只移出名单，
// 后续同样的上游错误会再次自动封禁（手动封禁解禁后不会自动恢复）。
const BLACKLIST_PLATFORM_LABEL = {
  workbuddy: 'WorkBuddy', zen: 'OpenCode', cline: 'Cline', trae: 'Trae',
  amd: 'AMDCloud', openrouter: 'OpenRouter', tokenharbor: 'TokenHarbor', other: 'Other'
};
// 已拉黑模型 ID 集合（对外 ID，如 wbgb:gpt-6-luna）。设置页各模型块渲染前
// 统一过滤——黑名单的语义是「不再对外提供」，不能只过滤 /v1/models 而把
// 设置页列表漏掉（此前 wb 块走 /admin/workbuddy/api/models 没过滤）。
let modelBlacklistIds = new Set();
async function syncModelBlacklist() {
  try {
    const d = await api('GET', '/models/blacklist');
    const items = (d.data || {}).items || [];
    modelBlacklistIds = new Set(items.map(i => i.model));
    return items;
  } catch (e) { return []; }
}
async function loadModelBlacklist() {
  const tb = _('blacklistBody');
  if (!tb) return;
  try {
    const items = await syncModelBlacklist();
    if (_('blacklistCount')) _('blacklistCount').textContent = items.length ? '· ' + items.length : '';
    if (!items.length) {
      tb.innerHTML = '<tr><td colspan="5" class="empty">' + T('No blocked models') + '</td></tr>';
      return;
    }
    tb.innerHTML = items.map(it => {
      const plat = BLACKLIST_PLATFORM_LABEL[it.platform] || it.platform;
      const when = it.createdAt ? new Date(it.createdAt).toLocaleString() : '—';
      // 悬停看上游原始报文 + 业务码；表格列只放统一文案（自动/手动封禁）。
      const title = esc([it.code ? 'code ' + it.code : '', it.detail || ''].filter(Boolean).join(' · ')).slice(0, 800);
      return '<tr>' +
        '<td>' + esc(plat) + '</td>' +
        '<td style="font-family:\'JetBrains Mono\',monospace" title="' + title + '">' + esc(it.model) + '</td>' +
        '<td style="font-size:12px;color:var(--text2)">' + esc(it.reason || '—') + '</td>' +
        '<td style="font-size:12px;color:var(--text2)">' + esc(when) + '</td>' +
        '<td style="text-align:center;white-space:nowrap">' +
          '<button class="btn btn-sm" data-bl-test="1" data-platform="' + esc(it.platform) + '" data-model="' + esc(it.model) + '">' + T('Test') + '</button> ' +
          '<button class="btn btn-sm btn-warn" data-bl-unblock="1" data-platform="' + esc(it.platform) + '" data-model="' + esc(it.model) + '">' + T('Unblock') + '</button>' +
        '</td></tr>';
    }).join('');
  } catch (e) {
    tb.innerHTML = '<tr><td colspan="5" class="empty">' + esc(T('Failed to load') + ': ' + e.message) + '</td></tr>';
  }
}
async function testBlockedModel(btn) {
  const platform = btn.dataset.platform, model = btn.dataset.model;
  const old = btn.textContent;
  btn.disabled = true; btn.textContent = T('Testing...');
  try {
    const d = await api('POST', '/models/blacklist/test', { platform: platform, model: model });
    const ok = d.success === true || (d.data && d.data.status === 'active');
    const info = (d.data && (d.data.error || d.data.reason)) || d.message || '';
    toast(model + ' → ' + (ok ? T('reachable — you can unblock it') : info), ok ? 'success' : 'error');
  } catch (e) {
    toast(e.message, 'error');
  } finally {
    btn.disabled = false; btn.textContent = old;
  }
}
async function unblockBlockedModel(btn) {
  const platform = btn.dataset.platform, model = btn.dataset.model, scope = btn.dataset.scope || '';
  if (!confirm(T('Unblock this model? It becomes available in /v1/models again; the same upstream error will ban it again.'))) return;
  btn.disabled = true;
  try {
    await api('POST', '/models/blacklist/unblock', { platform: platform, model: model, scope: scope });
    toast(T('Unblocked: ') + model, 'success');
    loadModelBlacklist();
  } catch (e) {
    toast(e.message, 'error'); btn.disabled = false;
  }
}
// banModel 手动封禁：模型展示区「封禁」按钮 → POST /models/blacklist/ban，
// 后端原因统一记「管理员手动封禁」。成功后刷新封禁表与当前所在页的模型列表。
async function banModel(btn) {
  const platform = btn.dataset.platform, model = btn.dataset.model, scope = btn.dataset.scope || '';
  if (!confirm(T('Ban this model? It is removed from /v1/models immediately; you can unblock it in the Banned models table.'))) return;
  btn.disabled = true;
  try {
    await api('POST', '/models/blacklist/ban', { platform: platform, model: model, scope: scope });
    toast(T('Banned: ') + model, 'success');
    loadModelBlacklist();
    const ref = btn.dataset.refresh;
    if (ref === 'amd') loadAmdConfig();
    else if (ref === 'or') loadOrConfig();
    else if (ref === 'th') loadTHConfig();
    else loadModels();
  } catch (e) {
    toast(e.message, 'error'); btn.disabled = false;
  }
}
document.addEventListener('click', e => {
  const t = e.target.closest ? e.target.closest('button[data-bl-test]') : null;
  if (t) { testBlockedModel(t); return; }
  const b = e.target.closest ? e.target.closest('button[data-bl-ban]') : null;
  if (b) { banModel(b); return; }
  const u = e.target.closest ? e.target.closest('button[data-bl-unblock]') : null;
  if (u) unblockBlockedModel(u);
});

async function refreshModels() {
  try {
    document.querySelectorAll('[data-models-info]').forEach(el => { el.textContent = '· syncing...'; });
    // 两个上游的模型源一起刷新（Cline feed + OpenCode 目录）
    const [d, z] = await Promise.all([
      api('POST', '/models/refresh').catch(e => ({ message: e.message })),
      api('POST', '/opencode/models/refresh').catch(e => ({ message: e.message }))
    ]);
    // 后端 Message 是固定短语（model sync started / synced, N new models），
    // 走 T() 翻译；N 为动态数字，拆前后半截分别翻。
    const tr = m => {
      if (!m) return m;
      let t = T(m);
      if (t !== m) return t;
      const mm = m.match(/^(synced, )(\d+)( new models)$/);
      if (mm) return T('synced, ') + mm[2] + T(' new models');
      return m;
    };
    const msg = [tr(d.message || (d.data && d.data.message)), tr(z.message || (z.data && z.data.message))].filter(Boolean).join(' | ');
    toast(msg || T('Sync started'), 'info');
    setTimeout(loadModels, 3000);
  } catch (e) { toast(T('Refresh failed: ') + e.message, 'error'); document.querySelectorAll('[data-models-info]').forEach(el => { el.textContent = ''; }); }
}

// ========== Banned platforms（封禁平台） ==========
// 整平台退出会话路由（platform_blacklist.go）：/v1/models 同步摘除、请求直接
// 503；签到/保活等维护任务不受影响。Other 的子平台可单独封禁（other:<slug>）。
// 长输入框 + chips（与代理排除规则卡片同款 tagbox 样式）+ 右侧「确认」按钮：
// 点选/去选只改暂存集（platformBansStaged），点「确认」一次性算差集批量下发
// （新增 → ban，取消 → unban）后统一落盘；chip 的 × 也只是取消暂存。
const PLATFORM_BAN_BASE = [
  { key: 'cline', label: 'Cline' },
  { key: 'trae', label: 'TraeWork' },
  { key: 'workbuddy', label: 'WorkBuddy' },
  { key: 'zen', label: 'OpenCode' },
  { key: 'amd', label: 'AMDCloud' },
  { key: 'openrouter', label: 'OpenRouter' },
  { key: 'tokenharbor', label: 'TokenHarbor' }
];
let platformBans = new Set();       // 服务器已落盘的封禁集
let platformBansStaged = new Set(); // 面板暂存集（点「确认」才下发）
async function loadPlatformBans() {
  const box = _('platformBanBox');
  if (!box) return;
  try {
    const d = await api('GET', '/platforms/blacklist');
    platformBans = new Set(((d.data || {}).items || []).map(i => i.platform));
  } catch (e) { platformBans = new Set(); }
  platformBansStaged = new Set(platformBans);
  if (!otherConfig) {
    try { await loadOtherConfig(); } catch (e) { otherConfig = { platforms: [] }; }
  }
  renderPlatformBan();
}
function platformBanOptions() {
  const out = PLATFORM_BAN_BASE.slice();
  ((otherConfig && otherConfig.platforms) || []).forEach(p => {
    out.push({ key: 'other:' + p.slug, label: p.name || p.slug, sub: true });
  });
  return out;
}
function renderPlatformBan() {
  const chips = _('platformBanChips'), menu = _('platformBanMenu');
  if (!chips || !menu) return;
  const opts = platformBanOptions();
  // chips：按选项序展示暂存集，× 取消暂存（点击不冒泡，避免开菜单）
  chips.innerHTML = opts.filter(o => platformBansStaged.has(o.key)).map(o =>
    '<span class="pr-chip" title="' + esc(T('Staged — click Save to save')) + '"><span title="' + esc(o.key) + '">' + esc(o.label) + '</span>' +
    '<button type="button" onclick="event.stopPropagation();stagePlatformBan(\'' + esc(o.key) + '\',false)" title="' + esc(T('Remove')) + '">×</button></span>'
  ).join('');
  const cnt = _('platformBanCount');
  if (cnt) cnt.textContent = platformBansStaged.size ? '· ' + platformBansStaged.size : '';
  menu.innerHTML = opts.map(o =>
    '<label class="pb-row' + (o.sub ? ' pb-row-sub' : '') + '">' +
      '<input type="checkbox"' + (platformBansStaged.has(o.key) ? ' checked' : '') +
      ' data-pb="' + esc(o.key) + '" data-pb-label="' + esc(o.label) + '" onchange="stagePlatformBan(this.dataset.pb, this.checked)">' +
      '<span>' + esc(o.label) + '</span>' +
    '</label>').join('');
}
function togglePlatformBanMenu(ev) {
  ev.stopPropagation();
  pbMenuToggle(_('platformBanMenu'), _('platformBanBox'), renderPlatformBan);
}
// 点击外部收起（选项区在菜单内不收起；菜单此时挂在 body 下）
document.addEventListener('click', ev => {
  const menu = _('platformBanMenu');
  if (!pbMenuIsOpen(menu)) return;
  if (menu.contains(ev.target)) return;
  const box = _('platformBanBox');
  if (box && box.contains(ev.target)) return;
  pbMenuHide(menu);
});
// pbMenuToggle/pbMenuHide 点选式下拉的开关：打开时把菜单挂到 body、fixed
// 定位到 anchor 正下方（同宽）。section 的 overflow:hidden 与 rise 动画层叠
// 上下文都会裁剪/盖住 section 内的绝对定位下拉（两次踩坑），body + fixed 是
// 唯一稳妥出路（同 .toast 机制）。滚动/缩放时收起（菜单自身滚动除外）。
// pbMenuOpenedAt 刚打开的宽限戳：scrollIntoView/锚点滚动产生的 scroll 事件
// 是异步派发的，会在开菜单后一帧内到达——没有宽限期菜单会被立刻关掉。
let pbMenuOpenedAt = 0;
function pbMenuHide(menu) { if (menu) menu.style.display = 'none'; }
function pbMenuIsOpen(menu) { return !!menu && menu.style.display === 'block'; }
function pbMenuToggle(menu, anchor, render) {
  if (!menu || !anchor) return;
  if (pbMenuIsOpen(menu)) { pbMenuHide(menu); return; }
  render();
  document.body.appendChild(menu);
  const r = anchor.getBoundingClientRect();
  menu.style.left = Math.round(r.left) + 'px';
  menu.style.top = Math.round(r.bottom + 4) + 'px';
  menu.style.width = Math.round(r.width) + 'px';
  menu.style.display = 'block';
  pbMenuOpenedAt = Date.now();
}
window.addEventListener('scroll', ev => {
  if (Date.now() - pbMenuOpenedAt < 200) return;
  const t = ev.target;
  if (t instanceof Element && t.closest('.pb-menu')) return; // 菜单自身滚动不关
  pbMenuHide(_('platformBanMenu'));
  pbMenuHide(_('ppBulkMenu'));
}, true);
window.addEventListener('resize', () => {
  pbMenuHide(_('platformBanMenu'));
  pbMenuHide(_('ppBulkMenu'));
});
// stagePlatformBan 暂存/取消暂存一个平台（不触 API，点「确认」才统一下发）
function stagePlatformBan(key, on) {
  if (on) platformBansStaged.add(key); else platformBansStaged.delete(key);
  renderPlatformBan();
}
// resetPlatformBans 复位按钮：放弃暂存改动，暂存集对齐已落盘集后重绘。
function resetPlatformBans() {
  platformBansStaged = new Set(platformBans);
  renderPlatformBan();
  toast(T('Staged selections reset'), 'info');
}
// applyPlatformBans 确认按钮：暂存集 vs 已落盘集算差集，新增 → ban、取消 →
// unban，全部成功后才把落盘集对齐暂存并重绘；中途失败则重拉服务器状态回滚
// 面板显示（部分成功的服务器状态以 GET 结果为准）。
async function applyPlatformBans() {
  const add = [...platformBansStaged].filter(k => !platformBans.has(k));
  const del = [...platformBans].filter(k => !platformBansStaged.has(k));
  if (!add.length && !del.length) { toast(T('No changes to apply'), 'info'); return; }
  try {
    for (const k of add) await api('POST', '/platforms/blacklist/ban', { platform: k });
    for (const k of del) await api('POST', '/platforms/blacklist/unban', { platform: k });
  } catch (e) {
    toast(T('Save failed: ') + e.message, 'error');
    try { await loadPlatformBans(); } catch (e2) { /* 重绘失败不影响错误提示 */ }
    return;
  }
  platformBans = new Set(platformBansStaged);
  renderPlatformBan();
  toast(T('Platform bans saved') + ' (+' + add.length + ' / -' + del.length + ')', 'success');
}

// ========== Config load ==========
async function loadConfig() {
  try {
    const d = await api('GET', '/config');
    const c = d.data;
    if (_('settingAdminLog')) _('settingAdminLog').checked = !!c.adminLogEnabled;
    if (c.poolPath && _('settingPoolPath')) _('settingPoolPath').value = c.poolPath;
    if (c.headers) {
      const tbody = _('headersTableBody');
      tbody.innerHTML = Object.entries(c.headers).map(([k, v]) =>
        '<tr>' +
          '<td><input type="text" class="header-key" value="' + esc(k) + '" style="font-size:12px;font-family:monospace;width:100%"></td>' +
          '<td><input type="text" class="header-val" value="' + esc(v) + '" style="font-size:12px;font-family:monospace;width:100%"></td>' +
          '<td><button class="btn btn-sm btn-danger" onclick="this.closest(\'tr\').remove()">Delete</button></td>' +
        '</tr>'
      ).join('');
    }
  } catch (e) { /* ignore */ }
}

// ========== opencode free models ==========
// Last-seen zen config; the proxy pool fields live on the Proxy pool page now,
// so saves must carry the cached values instead of removed form inputs.
let ocCfgCache = {};

async function loadOcConfig() {
  try {
    await loadProxyListCache();
    const d = await api('GET', '/opencode/config');
    const c = d.data;
    ocCfgCache = c;
    const ks = c.keyStates || [];
    // 标题下统计：key · 可用（路由启用）· 冷却中（与 Cline 页摘要同款）
    const sum = _('ocSummary');
    if (sum) {
      const routed = ks.filter(k => k.routingEnabled !== false).length;
      const cooling = ks.filter(k => k.cooling).length;
      sum.textContent = ks.length + ' ' + T('keys') + ' · ' + routed + ' ' + T('active') + ' · ' + cooling + ' ' + T('cooling');
    }
    renderOcKeyStates(ks);
    _('ocBaseURL').value = c.baseURL || '';
    _('ocMaxConc').value = c.maxConcurrency || 8;
    _('ocRetries').value = c.retries || 3;
    if (_('ocPinProxy')) _('ocPinProxy').value = String(c.pinProxyPerKey !== false);
    const eg = c.egress || {};
    if (_('ocEgressInfo')) {
      const un = eg.unassigned || 0;
      _('ocEgressInfo').innerHTML = !eg.enabled
        ? '<span style="color:var(--text2)">disabled — per-request rotation</span>'
        : (!eg.active
            ? '<span style="color:var(--text2)">no proxies — direct</span>'
            : '<span style="color:' + (un > 0 ? 'var(--danger)' : 'var(--accent2)') + '">' +
              (eg.proxies || 0) + ' prox' + ((eg.proxies || 0) === 1 ? 'y' : 'ies') + ' · ' +
              (eg.assigned || 0) + '/' + (eg.identities || 0) + ' keys bound' +
              (un > 0 ? ' · ' + un + ' key' + (un === 1 ? '' : 's') + ' skipped' : '') + '</span>');
    }
    _('ocCompactAuto').value = String(c.compaction ? c.compaction.auto : true);
    _('ocCompactBuffer').value = c.compaction ? c.compaction.buffer : 20000;
    _('ocKeepTokens').value = c.compaction ? c.compaction.keepTokens : 8000;
    _('ocSummaryModel').value = c.compaction ? (c.compaction.summaryModel || '') : '';
    _('ocMaxSummary').value = c.compaction ? c.compaction.maxSummary : 4096;
  } catch (e) { /* ignore */ }
}

async function saveOcConfig() {
  // key 列表不随本表单提交（增删在 key 表的添加凭据/删除按钮里），避免
  // 旧 textarea 快照覆盖面板当前的 key 池。
  const body = {
    baseURL: _('ocBaseURL').value.trim(),
    proxies: ocCfgCache.proxies || [],
    proxyStrategy: ocCfgCache.proxyStrategy || 'round_robin',
    maxConcurrency: parseInt(_('ocMaxConc').value) || 8,
    retries: parseInt(_('ocRetries').value) || 3,
    pinProxyPerKey: _('ocPinProxy') ? _('ocPinProxy').value === 'true' : true,
    compaction: {
      auto: _('ocCompactAuto').value === 'true',
      buffer: parseInt(_('ocCompactBuffer').value) || 20000,
      keepTokens: parseInt(_('ocKeepTokens').value) || 8000,
      summaryModel: _('ocSummaryModel').value.trim(),
      maxSummary: parseInt(_('ocMaxSummary').value) || 4096
    }
  };
  try {
    const d = await api('POST', '/opencode/config/update', body);
    toast('OpenCode config saved', 'success');
    loadOcConfig();
  } catch (e) { toast(T('Save failed: ') + e.message, 'error'); }
}

// per-key 状态表：key 掩码 / 今日累计 token / 最近使用 / 会话（ID + live 状态 +
// 下次轮换时刻）/ 冷却（含预计恢复时刻）/ 测试+删除。
// Test 与 cline 账号的同语义：真实探测，成功即复位该 key 的冷却。
// Proxy binding 列与 Pool（路由启用）列均为暂存编辑：改动行高亮，由
// "保存"按钮统一提交（隔离模式语义见 Proxy pool 页）。铸造跟随路由启用，
// 不再有独立的铸造开关列。
let keySnapshot = {};
function renderOcKeyStates(ks) {
  const tb = _('ocKeysBody');
  if (!tb) return;
  keySnapshot = {};
  tb.innerHTML = ks.length
    ? ks.map(k => {
        // 状态列（合并原 会话+冷却 两列，chip 样式与 cline 状态列一致）：
        // 冷却中（含预计恢复小字）> 会话活跃（会话 ID 与
        // 铸造时刻进 title）> 会话已失效（下次请求自动重铸）> 无会话（待启用，
        // 路由启用后自动铸造）。
        let stCell;
        if (k.cooling) {
          stCell = '<span class="status cooldown"><span class="status-dot cooldown"></span>' + T('Cooldown') + '</span>' +
            (k.cooldownUntil ? '<div style="font-size:10px;color:var(--text3);margin-top:2px;white-space:nowrap" title="' + esc(fmtWhen(k.cooldownUntil)) + '">' + T('Recovers ') + esc(fmtShort(k.cooldownUntil)) + '</div>' : '');
        } else if (k.session && k.sessionLive) {
          stCell = '<span class="status active" title="' + esc(T('session minted')) + ' ' + esc(fmtWhen(k.sessionCreatedAt)) + ' · ' + esc(k.session) + '…"><span class="status-dot active"></span>' + T('Active') + '</span>';
        } else if (k.session) {
          stCell = '<span class="status pending" title="' + esc(T('session minted')) + ' ' + esc(fmtWhen(k.sessionCreatedAt)) + '"><span class="status-dot pending"></span>' + T('stale') + '</span>';
        } else {
          stCell = '<span class="status pending"><span class="status-dot pending"></span>' + T('Pending activation') + '</span>';
        }
        // omitempty 下未绑定 key 没有这两个键，归一化为 ''
        const pMain = k.proxyMain || '';
        const pBackup = k.proxyBackup || '';
        // 备用代理开关关闭 → 备用槽只读跟随主槽（key 表与账号表同规则）
        const backupLocked = !backupProxyOn || pMain === '' || pMain === EGRESS_DIRECT;
        const backupVal = backupLocked ? pMain : pBackup;
        const isStale = proxyStale;
        const bind = '<td style="white-space:nowrap">' +
          egComboHTML('key', k.index, 'main', pMain, T('Main exit — used whenever available')) + ' ' +
          egComboHTML('key', k.index, 'backup', backupVal, T('Backup exit — used when main is cooling/removed'), backupLocked) +
          (isStale(pMain) || isStale(pBackup) ? ' <span title="' + esc(T('Bound proxy is no longer in the proxy pool — skipped until fixed')) + '" style="color:var(--danger)">⚠</span>' : '') +
          '</td>';
        // Pool（路由启用）列：勾选后该 key 才参与请求轮转；铸造跟随路由，
        // 路由关闭的 key 也不再被自动铸造。
        const routed = k.routingEnabled !== false;
        keySnapshot[k.index] = { main: pMain, backup: backupVal, routing: routed };
        const routing = '<td style="text-align:center">' +
          '<input type="checkbox" data-zr="' + k.index + '"' + (routed ? ' checked' : '') + ' title="' + esc(T('Participates in request routing')) + '"></td>';
        // 操作列：测试（真实探测）+ 删除（红色，从 key 池移除该 key）
        const actions = '<td style="white-space:nowrap;text-align:center">' +
          '<button class="btn btn-sm" data-zk="' + k.index + '">Test</button> ' +
          '<button class="btn btn-sm btn-danger" data-zdel="' + k.index + '" title="' + esc(T('Remove this key from the pool')) + '">Delete</button></td>';
        return '<tr draggable="true" data-drag-id="' + k.index + '"><td>#' + (k.index + 1) + (k.current ? ' <span style="color:var(--accent)" title="' + esc(T('next in rotation')) + '">●</span>' : '') + '</td>' +
          '<td style="font-family:monospace;font-size:11px">' + esc(k.keyMask) + '</td>' +
          '<td>' + stCell + '</td>' +
          keyTokensCell(k) +
          keyLastUsedCell(k) +
          bind +
          actions +
          routing + '</tr>';
      }).join('')
    : '<tr><td colspan="8" class="empty">No opencode keys configured</td></tr>';
  tb.onclick = e => {
    const b = e.target.closest('button[data-zk]');
    if (b) { testZenKey(parseInt(b.dataset.zk, 10), b); return; }
    const d = e.target.closest('button[data-zdel]');
    if (d) deleteZenKey(parseInt(d.dataset.zdel, 10), d);
  };
  tb.onchange = e => {
    const r = e.target.closest('input[data-zr]');
    if (r) markRowDirty(r.closest('tr'), 'key');
  };
}

function stageAllKeysRouting(enabled) {
  document.querySelectorAll('#ocKeysBody input[data-zr]').forEach(cb => {
    const snap = keySnapshot[parseInt(cb.dataset.zr, 10)];
    if (snap && snap.routing === enabled) return;
    cb.checked = enabled;
    markRowDirty(cb.closest('tr'), 'key');
  });
}
async function saveKeyEdits() {
  const rows = [...document.querySelectorAll('#ocKeysBody tr')].filter(r => r.querySelector('.eg-input'));
  let changed = 0, failed = 0;
  for (const row of rows) {
    const mainInput = row.querySelector('.eg-input[data-eg="key"][data-slot="main"]');
    const idx = parseInt(mainInput.dataset.egid, 10);
    const main = mainInput.dataset.value || '';
    const backup = row.querySelector('.eg-input[data-eg="key"][data-slot="backup"]').dataset.value || '';
    const rcb = row.querySelector('input[data-zr]');
    const snap = keySnapshot[idx] || { main: '', backup: '', routing: true };
    try {
      if (main !== snap.main || backup !== snap.backup) {
        await api('POST', '/opencode/keys/proxy', { index: idx, main: main, backup: backup });
      }
      if (rcb && rcb.checked !== snap.routing) {
        await api('POST', '/opencode/keys/routing', { index: idx, enabled: rcb.checked });
      }
      if (main !== snap.main || backup !== snap.backup || (rcb && rcb.checked !== snap.routing)) changed++;
      row.classList.remove('tr-dirty');
    } catch (e) { failed++; }
  }
  clearDirtyMarks('ocKeysBody', 'ocKeysSaveBtn');
  if (failed) toast(T('Save failed for ') + failed + T(' account(s)'), 'error');
  else if (!changed) toast(T('No changes to save'), 'info');
  else toast(T('Saved ') + changed + T(' account change(s)'), 'success');
  loadOcConfig();
}

// 复位：丢弃 key 表的全部暂存修改，按上次保存的状态重绘
function resetKeyEdits() {
  clearDirtyMarks('ocKeysBody', 'ocKeysSaveBtn');
  loadOcConfig();
}

// 删除单个 key：从 key 池移除并按索引刷新（索引会前移，直接重拉）。
async function deleteZenKey(index, btn) {
  if (!confirm(T('Delete this key?'))) return;
  const original = btn ? btn.innerHTML : '';
  if (btn) btn.disabled = true;
  try {
    await api('POST', '/opencode/keys/delete', { index: index });
    toast(T('Key #') + (index + 1) + T(' (deleted)'), 'success');
  } catch (e) {
    toast(T('Delete failed: ') + e.message, 'error');
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = original; }
    loadOcConfig();
  }
}

// 批量测试：等价于对每个 key 点一次 Test（真实探测，成功即复位冷却）。
// 逐个串行避免打爆上游，按钮显示进度。
async function batchTestKeys() {
  const btn = _('ocTestBtn');
  const idxs = Object.keys(keySnapshot).map(Number).sort((a, b) => a - b);
  if (!idxs.length) { toast(T('No keys to test'), 'info'); return; }
  const original = btn ? btn.innerHTML : '';
  if (btn) btn.disabled = true;
  let ok = 0, cool = 0, err = 0;
  try {
    for (let i = 0; i < idxs.length; i++) {
      if (btn) btn.innerHTML = '<span class="loading"></span>' + (i + 1) + '/' + idxs.length;
      try {
        const d = await api('POST', '/zen/keys/test', {
          index: idxs[i],
          model: (_('ocProbeModel') ? _('ocProbeModel').value : '')
        });
        const r = d.data || {};
        if (r.status === 'active') ok++;
        else if (r.status === 'cooldown') cool++;
        else err++;
      } catch (e) { err++; }
    }
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = original; }
  }
  toast(T('Batch test done: ') + idxs.length + T(' tested, ') + ok + T(' ok') +
    (cool ? T(', ') + cool + T(' cooling') : '') + (err ? T(', ') + err + T(' error') : ''),
    err ? 'warning' : 'success', 6000);
  loadOcConfig();
}

// ========== 添加凭据弹窗（key 逐行填入文本域，合并进当前 key 池） ==========
function openZenKeyAddDialog() {
  _('zenKeyAddInput').value = '';
  _('zenKeyAddDialog').showModal();
}
function closeZenKeyAddDialog() { _('zenKeyAddDialog').close(); }
async function zenKeyAddFromDialog() {
  const lines = _('zenKeyAddInput').value.split('\n').map(s => s.trim()).filter(Boolean);
  if (!lines.length) { toast(T('Enter at least one key'), 'error'); return; }
  const cur = (ocCfgCache.keys && ocCfgCache.keys.length) ? ocCfgCache.keys : (ocCfgCache.key ? [ocCfgCache.key] : []);
  const fresh = lines.filter(l => !cur.includes(l));
  if (!fresh.length) { toast(T('No new keys (all already in the pool)'), 'info'); return; }
  const btn = _('zenKeyAddSubmit');
  btn.disabled = true;
  try {
    await api('POST', '/opencode/config/update', { keys: cur.concat(fresh) });
    toast(T('Added ') + fresh.length + T(' key(s)'), 'success');
    closeZenKeyAddDialog();
    loadOcConfig();
  } catch (e) {
    toast(T('Save failed: ') + e.message, 'error');
  } finally {
    btn.disabled = false;
  }
}

async function testZenKey(index, btn) {
  const original = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span>Testing'; }
  try {
    const d = await api('POST', '/zen/keys/test', {
      index: index,
      // 下拉里的探测模型；空串 = 后端自动选（big-pickle → live → 种子）
      model: (_('ocProbeModel') ? _('ocProbeModel').value : '')
    });
    const r = d.data || {};
    const label = { active: T('OK'), cooldown: T('Cooldown'), error: T('Error') }[r.status] || r.status;
    let msg = T('Key #') + (index + 1) + T(' (') + (r.keyMask || '') + T(') — ') + label;
    if (r.model) msg += T(' via ') + r.model;
    if (r.latencyMs != null) msg += ' (' + r.latencyMs + 'ms)';
    if (r.status === 'active') msg += T(' — cooldown cleared');
    if (r.cooldownUntil) msg += T('\nEstimated recovery: ') + fmtWhen(r.cooldownUntil) + (r.remaining ? T(' (remaining ') + r.remaining + T(')') : '');
    if (r.reason && r.reason !== 'ok') msg += '\n' + r.reason;
    const type = r.status === 'active' ? 'success' : (r.status === 'cooldown' ? 'warning' : 'error');
    toast(msg, type, 6000);
  } catch (e) {
    toast(T('Test failed: ') + e.message, 'error');
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = original; }
    loadOcConfig();  // 冷却被清除/新设置，立即刷新状态表
  }
}

// ========== Proxy pool ==========
async function loadProxyPool() {
  try {
    const [oc, cfg] = await Promise.all([api('GET', '/opencode/config'), api('GET', '/config')]);
    const c = oc.data;
    proxyListCache = (c.proxies || []).slice();
    syncProxyHealthCache((c.runtime || {}).proxyHealth);
    backupProxyOn = c.backupProxyEnabled === true;
    proxyExclusions = c.proxyExclusions || {};
    proxyBindCaps = c.proxyBindCaps || {};
    renderProxyRules();
    _('ppProxies').value = (c.proxies || []).join('\n');
    _('ppStrategy').value = c.proxyStrategy || 'round_robin';
    _('ppSubURL').value = c.proxySubURL || '';
    _('ppSubCron').value = c.proxySubCron || '';
    syncProxyListEditable();
    _('ppCline').value = String(!!cfg.data.clineUseProxies);
    _('ppIsolation').value = String(c.proxyIsolation !== false);
    _('ppBackupEnabled').checked = c.backupProxyEnabled === true;
    if (c.proxyIsolationEnvLocked) {
      _('ppIsolation').disabled = true;
      _('ppIsolationHint').innerHTML = '<span style="color:var(--warning,#eab308)">PROXY_ISOLATION=true is set — isolation is forced on. Unset the env var (or set it to false) to control isolation from the panel.</span>';
    } else {
      _('ppIsolation').disabled = false;
      _('ppIsolationHint').textContent = '';
    }
    _('ppZen').value = String(c.zenUseProxies !== false);
    _('ppWb').value = String(c.workbuddyUseProxies !== false);
    // Trae 走池开关在 /trae/config（独立配置）
    try {
      const trc = await api('GET', '/trae/config');
      _('ppTrae').value = String(trc.data.useProxies !== false);
    } catch (e) { _('ppTrae').value = 'true'; }
    // OpenRouter 走池开关在 /openrouter/config（独立配置，不复用 zen 配置）
    try {
      const orc = await api('GET', '/openrouter/config');
      _('ppOr').value = String(orc.data.useProxies !== false);
    } catch (e) { _('ppOr').value = 'true'; }
    try {
      const amdc = await api('GET', '/amd/config');
      _('ppAmd').value = String(amdc.data.useProxies !== false);
    } catch (e) { _('ppAmd').value = 'true'; }
    try {
      const thc = await api('GET', '/tokenharbor/config');
      _('ppTh').value = String(thc.data.useProxies !== false);
    } catch (e) { _('ppTh').value = 'true'; }
    try {
      const oc = await api('GET', '/other/config');
      _('ppOther').value = String(oc.data.useProxies !== false);
    } catch (e) { _('ppOther').value = 'true'; }
    // 展示行与后端池保持一致（只列当前池内代理），数值取本地合并快照——
    // 池编辑清零/代理暂离期间由留存层兜底，代理回归后旧数据自动恢复显示
    renderProxyHealth(proxyHealthBackendRows.map(r => proxyHealthByProxy[proxyHealthKey(r.proxy)]).filter(Boolean));
  } catch (e) { /* ignore */ }
}

// 每代理在线率表：纯被动统计（真实请求的出口成败），编辑代理池即清零。
// 在线率上色与代理下拉框同源（proxyRateTier 五档），只改展示不改路由
// 行为——换不换代理由人决定。
// ppHsSort 平均握手延迟列排序状态：0=默认(后端顺序) 1=升序 2=降序；点击表头循环切换。
let ppHsSort = 0;
function ppHsSortToggle() {
  ppHsSort = (ppHsSort + 1) % 3;
  const mark = _('ppHsSortMark');
  if (mark) mark.textContent = ppHsSort === 1 ? ' ▲' : ppHsSort === 2 ? ' ▼' : '';
  renderProxyHealth(Object.values(proxyHealthByProxy));
}
function renderProxyHealth(rowsIn) {
  const tb = _('ppHealthBody');
  if (!tb) return;
  // 延迟列排序：无样本（null/undefined）恒排最后，与升降序无关
  let rows = rowsIn;
  if (ppHsSort) {
    rows = rowsIn.slice().sort((a, b) => {
      const na = a.avgHsMs === null || a.avgHsMs === undefined;
      const nb = b.avgHsMs === null || b.avgHsMs === undefined;
      if (na && nb) return 0;
      if (na) return 1;
      if (nb) return -1;
      return ppHsSort === 1 ? a.avgHsMs - b.avgHsMs : b.avgHsMs - a.avgHsMs;
    });
  }
  tb.innerHTML = rows.length
    ? rows.map(r => {
        const pct = rate => (rate === null || rate === undefined) ? null : Math.round(rate * 100) + '%';
        // 在线率上色与代理下拉框同源：五档底色（proxyRateTier），累计/最近
        // 两列口径一致，不再单用"低率标红"。
        const rateCell = (v, rate) => v === null
          ? '<span style="color:var(--text3)">-</span>'
          : '<span class="pp-rate t-' + proxyRateTier(rate) + (v.length > 3 ? ' w3' : '') + '">' + esc(v) + '</span>';
        const cum = pct(r.rate);
        const rec = pct(r.recentRate);
        // Avg HS：成功尝试的平均握手延迟（ms），无样本显示 -。
        const hs = (r.avgHsMs === null || r.avgHsMs === undefined)
          ? '<span style="color:var(--text3)">-</span>'
          : '<span class="mono" style="font-size:11px">' + Math.round(r.avgHsMs) + ' ms</span>';
        const failTitle = r.lastErr ? ' title="' + esc(r.lastErr) + '"' : '';
        return '<tr>' +
          '<td class="mono" style="font-size:11px">' + esc(egLabel(r.proxy)) + '</td>' +
          '<td style="text-align:center">' + (r.attempts || 0) + '</td>' +
          '<td style="text-align:center">' + rateCell(cum, r.rate) + '</td>' +
          '<td style="text-align:center">' + rateCell(rec, r.recentRate) + '</td>' +
          '<td style="text-align:center" title="' + esc(T('Avg tunnel-setup time of successful attempts (TCP+TLS+CONNECT), excluding upstream response; samples: ')) + (r.hsCount || 0) + '">' + hs + '</td>' +
          '<td class="mono" style="font-size:11px">' + (r.lastOk ? fmtWhen(r.lastOk) : '<span style="color:var(--text3)">-</span>') + '</td>' +
          '<td class="mono" style="font-size:11px"' + failTitle + '>' + (r.lastFail ? fmtWhen(r.lastFail) : '<span style="color:var(--text3)">-</span>') + '</td>' +
          '<td>' + (r.cooling ? '<span style="color:var(--warning,#eab308)">' + T('cooldown until ') + fmtWhen(r.coolingUntil) + '</span>' : '<span style="color:var(--text3)">-</span>') + '</td>' +
        '</tr>';
      }).join('')
    : '<tr><td colspan="8" class="empty">No proxies configured</td></tr>';
}

// syncProxyListEditable 订阅链接有值时代理列表只读（池由订阅管理，
// 列表现值仅在拉取失败时兜底），清空订阅链接后恢复可编辑。
function syncProxyListEditable() {
  const ta = _('ppProxies');
  if (!ta) return;
  const locked = !!(_('ppSubURL').value || '').trim();
  ta.readOnly = locked;
  ta.style.opacity = locked ? '.55' : '';
  ta.title = locked ? T('Managed by the subscription URL — clear it to edit manually') : '';
}
async function saveProxyPool() {
  const proxies = _('ppProxies').value.split('\n').map(s => s.trim()).filter(Boolean);
  const subUrl = _('ppSubURL').value.trim();
  if (!subUrl) {
    const PROXY_RE = /^(https?|socks5h?|socks):\/\/[^\s]+:\d+/;
    const bad = proxies.find(p => !PROXY_RE.test(p));
    if (bad) { toast(T('Invalid proxy format: ') + bad + ' (need http(s)://host:port or socks5://host:port)', 'error'); return; }
  }
  try {
    const payload = { proxyStrategy: _('ppStrategy').value, proxySubURL: subUrl, proxySubCron: _('ppSubCron').value.trim() };
    // 订阅有值时不下发 proxies（后端保留现池，池只由订阅覆盖）
    if (!subUrl) payload.proxies = proxies;
    await api('POST', '/opencode/config/update', payload);
    toast('Proxy pool saved', 'success');
    loadProxyPool();
  } catch (e) { toast(T('Save failed: ') + e.message, 'error'); }
}

// ===== 代理排除规则（平台黑名单 / 每代理上限）=====
// 本地编辑态：每平台 { cap: 数值, ex: [池条目...] }。renderProxyRules 从
// proxyExclusions/proxyBindCaps 反转重建（只保留池内条目——失效规则不展示、
// 不回存），保存时再反转回 proxy -> [kind...] 整表提交。
let ppRulesCache = null;
let prOtherOpen = true;     // Other 分组展开态（默认展开，否则子平台配置不可见）
let prActiveKind = '';      // 当前聚焦的标签框（重渲染后据此恢复焦点与筛选串）
let prQuery = '';

// ruleLeaves 真正承载规则的行：6 个基础平台 + Other 各子平台。
// Other 在面板上只是归类行 —— 它本身不写规则，子平台按 "other:<slug>" 独立配置。
function ruleLeaves() {
  const out = REORDER_PLATFORMS.filter(r => r.key !== 'other').map(r => ({ key: r.key, label: r.label }));
  ((otherConfig && otherConfig.platforms) || []).forEach(p => {
    out.push({ key: 'other:' + p.slug, label: p.name || p.slug, sub: true, prefix: p.prefix });
  });
  return out;
}

function ppRulesState() {
  if (!ppRulesCache) {
    ppRulesCache = {};
    ruleLeaves().forEach(r => {
      // 兼容旧的整平台 "other" 规则：子平台未单独配置时继承它，保存后即归一为
      // 子平台条目（Other 归类行不再写 kind=other）。
      const legacy = r.sub ? 'other' : null;
      ppRulesCache[r.key] = {
        cap: proxyCapFor(r.key) || (legacy ? proxyCapFor(legacy) : 0),
        ex: proxyListCache.filter(p => proxyExcludedFor(p, r.key) || !!(legacy && proxyExcludedFor(p, legacy))),
        dx: proxyExcludedFor(EGRESS_DIRECT, r.key) || !!(legacy && proxyExcludedFor(EGRESS_DIRECT, legacy))
      };
    });
  }
  return ppRulesCache;
}

// prMenuRows 卡片候选菜单行：搜索口径与批量追加框一致（别名或代理原始串任一
// 包含即命中）。抽成独立函数供 prCard 渲染与 ppRuleInput 输入时就地刷新共用——
// 输入路径绝不重建输入框节点（见 ppRuleInput）。
function prMenuRows(kind, st) {
  const left = proxyListCache.filter(p => p !== EGRESS_DIRECT && st.ex.indexOf(p) < 0);
  const q = prQuery.toLowerCase();
  const shown = left.filter(p => !q || egSearchText(p).includes(q));
  if (!shown.length) {
    return '<div class="pr-opt-empty">' + T(left.length ? 'No match' : 'No pool proxies left to exclude') + '</div>';
  }
  return shown.map(p => {
    const host = maskProxyLabel(p).replace(/^[a-z0-9]+:\/\//i, '').split(/[\/?#]/)[0];
    return '<div class="pr-opt" data-kind="' + esc(kind) + '" data-proxy="' + esc(p) + '"' +
      ' onmousedown="event.preventDefault()" onclick="ppRulePick(this.dataset.kind,this.dataset.proxy)">' +
      '<span>' + esc(egLabel(p)) + '</span><span class="pr-opt-host">' + esc(host) + '</span></div>';
  }).join('');
}

// prExSorted 卡片已排除条目按代理池顺序输出：暂存态是"代理 -> 数组"的追加
// 语义（新 pick 的 push 到尾部），直接渲染会按加入次序排；以 proxyListCache
// （池序）为骨架过滤一遍，chips 与候选菜单/绑定下拉同一顺序可对照。
function prExSorted(ex) {
  const inSt = p => ex.indexOf(p) >= 0;
  const sorted = proxyListCache.filter(inSt);
  // 池外残留（后端还有、池里已删的条目）不展示也不回存，与旧口径一致
  return sorted;
}

// prCard 单个平台/子平台卡片：标题行（名称 + 上限 + 排除直连开关）+ 标签输入框。
function prCard(leaf, s) {
  const k = leaf.key;
  const exSorted = prExSorted(s.ex);
  const chips = exSorted.map((p, i) =>
    '<span class="pr-chip" title="' + esc(egLabel(p)) + '">' + esc(egLabel(p)) +
    '<button type="button" data-kind="' + esc(k) + '" data-proxy="' + esc(encodeURIComponent(p)) + '"' +
    ' onmousedown="event.preventDefault()" onclick="ppRuleDelEx(this.dataset.kind,decodeURIComponent(this.dataset.proxy))"' +
    ' title="' + esc(T('Remove')) + '">×</button></span>').join('');
  const q = prActiveKind === k ? prQuery : '';
  const menu = prMenuRows(k, s);
  return '<div class="pr-card' + (leaf.sub ? ' pr-card-sub' : '') + '">' +
    '<div class="pr-card-head">' +
      '<span class="pr-name" title="' + esc(leaf.prefix ? k + ' · ' + leaf.prefix : k) + '">' + esc(leaf.label) + '</span>' +
      '<span class="pr-ctl">' +
        '<span class="pr-capbox"><span class="pr-unit">' + T('Max reuse per proxy') + '</span>' +
        '<input class="pr-cap" id="ppCap-' + esc(k) + '" type="number" min="0" step="1" placeholder="1" value="' + (s.cap || 1) + '"' +
        ' oninput="ppRuleCapInput(\'' + esc(k) + '\',this.value)">' +
        '<span class="pr-unit">' + T('times') + '</span></span>' +
        '<label class="pr-switch" title="' + esc(T('Exclude Direct')) + '"><input type="checkbox"' + (s.dx ? ' checked' : '') +
        ' onchange="ppRuleToggleDirect(\'' + esc(k) + '\',this.checked)"><span>' + esc(T('Exclude Direct')) + '</span></label>' +
      '</span>' +
    '</div>' +
    '<div class="pr-tagbox" onclick="ppRuleFocus(\'' + esc(k) + '\')">' + chips +
      '<input class="pr-tagin" id="prTagIn-' + esc(k) + '" value="' + esc(q) + '"' +
      ' placeholder="' + esc(chips ? '' : T('Search and add a proxy…')) + '"' +
      ' onfocus="ppRuleFocus(\'' + esc(k) + '\')" oninput="ppRuleInput(\'' + esc(k) + '\',this.value)"' +
      ' onkeydown="ppRuleKey(event,\'' + esc(k) + '\')">' +
    '</div>' +
    (prActiveKind === k ? '<div class="pr-menu">' + menu + '</div>' : '') +
    '</div>';
}

function renderProxyRules() {
  const tb = _('ppRulesBody');
  if (!tb) return;
  // Other 子平台清单来自 /other/config：未加载时先拉一次再渲染（只拉一次，
  // 失败则退化为只有 6 个基础平台，不阻塞其余模块）。
  if (!otherConfig) {
    if (!tb.dataset.loading) {
      tb.dataset.loading = '1';
      tb.innerHTML = '<div class="pr-none" style="padding:10px 12px">Loading...</div>';
      loadOtherConfig().then(() => { ppRulesCache = null; renderProxyRules(); })
        .catch(() => { otherConfig = { platforms: [] }; ppRulesCache = null; renderProxyRules(); });
    }
    return;
  }
  const st = ppRulesState();
  const leaves = ruleLeaves();
  const base = leaves.filter(l => !l.sub);
  const subs = leaves.filter(l => l.sub);
  const groupHead = '<div class="pr-group-head" onclick="ppRuleToggleGroup()">' +
      '<svg class="pr-chev" viewBox="0 0 24 24"><path d="M9 6l6 6-6 6"/></svg>' +
      '<span class="pr-group-name">Other</span>' +
      '<span class="pr-group-meta">' + (subs.length
        ? subs.length + T(' sub-platform(s) · each configured separately')
        : T('No sub-platform yet')) + '</span>' +
    '</div>';
  tb.innerHTML =
    '<div class="pr-grid">' + base.map(l => prCard(l, st[l.key])).join('') + '</div>' +
    '<div class="pr-group' + (prOtherOpen ? ' open' : '') + '">' + groupHead +
      (prOtherOpen && subs.length
        ? '<div class="pr-group-body">' + subs.map(l => prCard(l, st[l.key])).join('') + '</div>' : '') +
    '</div>';
  if (prActiveKind) {
    const inp = _('prTagIn-' + prActiveKind);
    if (inp) { inp.focus(); inp.setSelectionRange(inp.value.length, inp.value.length); }
  }
  // 卡片增删会改变「已在所有平台排除」的集合，批量追加框的回显 chips 随之同步
  ppBulkSyncChips();
}

function ppRuleToggleGroup() { prOtherOpen = !prOtherOpen; renderProxyRules(); }
function ppRuleFocus(kind) {
  if (prActiveKind !== kind) { prActiveKind = kind; prQuery = ''; }
  renderProxyRules();
}
// ppRuleInput 输入时只刷新候选菜单（prMenuRows），绝不重建输入框节点——
// 旧实现每个键都 renderProxyRules 重建整个网格：快速输入丢键、中文输入法
// 组合被打断，体感即「打好几个字符搜索才生效」。焦点在别的卡片时才走全量
// 渲染收口（正常输入路径焦点必然已在本卡片）。
function ppRuleInput(kind, v) {
  prQuery = v;
  if (prActiveKind !== kind) { prActiveKind = kind; renderProxyRules(); return; }
  const inp = _('prTagIn-' + kind);
  const menu = inp ? inp.closest('.pr-card').querySelector('.pr-menu') : null;
  if (!menu) { renderProxyRules(); return; }
  menu.innerHTML = prMenuRows(kind, ppRulesState()[kind]);
}
function ppRulePick(kind, proxy) {
  if (!proxy) return;
  const st = ppRulesState();
  if (!st[kind]) return;
  if (st[kind].ex.indexOf(proxy) < 0) st[kind].ex.push(proxy);
  prActiveKind = kind; prQuery = '';
  renderProxyRules();
}
function ppRuleKey(ev, kind) {
  const st = ppRulesState()[kind];
  if (!st) return;
  if (ev.key === 'Escape') { prActiveKind = ''; prQuery = ''; renderProxyRules(); return; }
  if (ev.key === 'Backspace' && !prQuery && st.ex.length) {
    st.ex.pop(); renderProxyRules(); return;
  }
  if (ev.key === 'Enter') {
    ev.preventDefault();
    const q = prQuery.toLowerCase();
    const cand = proxyListCache.filter(p => p !== EGRESS_DIRECT && st.ex.indexOf(p) < 0 &&
      egSearchText(p).includes(q));
    if (cand.length) ppRulePick(kind, cand[0]);
  }
}
function ppRuleCapInput(kind, v) {
  const st = ppRulesState()[kind];
  if (st) st.cap = parseInt(v, 10) || 0;
}
function ppRuleToggleDirect(kind, on) {
  const st = ppRulesState()[kind];
  if (st) st.dx = !!on;
}
function ppRuleDelEx(kind, proxy) {
  const st = ppRulesState()[kind];
  if (!st || !proxy) return;
  const i = st.ex.indexOf(proxy);
  if (i < 0) return;
  st.ex.splice(i, 1);
  renderProxyRules();
}
// 点卡片外部收起候选菜单（捕获阶段之外的冒泡即可；选项自身已 preventDefault）
document.addEventListener('mousedown', ev => {
  if (!prActiveKind) return;
  const card = ev.target && ev.target.closest ? ev.target.closest('.pr-card') : null;
  const inp = _('prTagIn-' + prActiveKind);
  if (card && inp && card.contains(inp)) return;
  prActiveKind = ''; prQuery = '';
  renderProxyRules();
});

async function saveProxyRules() {
  const st = ppRulesState();
  const exclusions = {}, caps = {};
  ruleLeaves().forEach(r => {
    (st[r.key].ex || []).forEach(p => {
      (exclusions[p] = exclusions[p] || []).push(r.key);
    });
    if (st[r.key].dx) {
      (exclusions[PROXY_RULE_DIRECT_KEY] = exclusions[PROXY_RULE_DIRECT_KEY] || []).push(r.key);
    }
    const el = _('ppCap-' + r.key);
    const cap = el && el.value !== '' ? (parseInt(el.value, 10) || 0) : (st[r.key].cap || 0);
    if (cap > 0) caps[r.key] = cap;
  });
  try {
    await api('POST', '/opencode/config/update', { proxyExclusions: exclusions, proxyBindCaps: caps });
    toast(T('Saved proxy exclusion rules'), 'success');
    loadProxyPool();
  } catch (e) { toast(T('Save failed: ') + e.message, 'error'); }
}

// ===== 批量追加排除（可搜索多选输入框 + 右侧按钮）=====
// 交互与下方各平台卡片的排除代理输入框同款：输入即时过滤候选、Enter 选第一个
// 匹配、Backspace（空查询时）删最后一个 chip、Esc 关闭；选中项以 chips 回显，
// 点右侧「批量追加」一键进入所有平台（含 Other 子平台）卡片的排除列表。与逐
// 卡片点选同一条暂存链路：只改本地 ppRulesCache，点「保存代理排除规则」才落
// 盘；已在所有平台排除的代理无可追加、不进菜单，但以置灰 chip 回显在本框中
// （× = 从所有平台批量取消，同为暂存）。直连不在此列（各卡片的「排除直连」开关单独管）。
const ppBulkSel = new Set();
let ppBulkQuery = '';
// ppBulkBoxClick 点框任意处 = 聚焦输入框并开菜单（已开则只聚焦，不反复开关）
function ppBulkBoxClick(ev) {
  ev.stopPropagation();
  if (!pbMenuIsOpen(_('ppBulkMenu'))) ppBulkOpenMenu(true);
  else { const i = _('ppBulkIn'); if (i) i.focus(); }
}
function ppBulkFocus() {
  if (!pbMenuIsOpen(_('ppBulkMenu'))) ppBulkOpenMenu(false);
}
// ppBulkOpenMenu 开菜单（重置查询串）并按需聚焦输入框；菜单挂 body（pbMenuToggle）
function ppBulkOpenMenu(focusInput) {
  ppBulkQuery = '';
  const inp = _('ppBulkIn');
  if (inp) inp.value = '';
  pbMenuToggle(_('ppBulkMenu'), _('ppBulkBox'), ppBulkRenderMenu);
  if (focusInput && pbMenuIsOpen(_('ppBulkMenu'))) {
    const i = _('ppBulkIn');
    if (i) i.focus();
  }
}
function ppBulkInput(v) {
  ppBulkQuery = v;
  const menu = _('ppBulkMenu');
  if (!pbMenuIsOpen(menu)) {
    pbMenuToggle(menu, _('ppBulkBox'), ppBulkRenderMenu);
    if (pbMenuIsOpen(menu)) { const i = _('ppBulkIn'); if (i) i.focus(); }
  } else {
    ppBulkRenderMenu();
  }
}
// 键盘：Enter 选第一个匹配候选；空查询 Backspace 删最后一个 chip；Esc 关闭
function ppBulkKey(ev) {
  if (ev.key === 'Escape') {
    pbMenuHide(_('ppBulkMenu'));
    ppBulkQuery = '';
    const i = _('ppBulkIn');
    if (i) i.value = '';
    return;
  }
  if (ev.key === 'Backspace' && !ppBulkQuery && ppBulkSel.size) {
    const last = [...ppBulkSel].pop();
    ppBulkSel.delete(last);
    ppBulkSyncChips();
    ppBulkRenderMenu();
    return;
  }
  if (ev.key === 'Enter') {
    ev.preventDefault();
    const q = ppBulkQuery.toLowerCase();
    const st = ppRulesState();
    const leaves = ruleLeaves();
    const cand = proxyListCache.find(p => p !== EGRESS_DIRECT && !ppBulkSel.has(p) &&
      !leaves.every(l => (st[l.key].ex || []).indexOf(p) >= 0) &&
      egLabel(p).toLowerCase().indexOf(q) >= 0);
    if (cand) {
      ppBulkPick(cand, true);
      ppBulkQuery = '';
      const i = _('ppBulkIn');
      if (i) i.value = '';
      ppBulkRenderMenu();
    }
  }
}
// ppBulkEverywhere 已在所有平台（含 Other 子平台）排除列表里的池内代理
// （直连不参与——它由各卡片的「排除直连」开关单独管）。
function ppBulkEverywhere() {
  const st = ppRulesState();
  const leaves = ruleLeaves();
  return proxyListCache.filter(p => p !== EGRESS_DIRECT &&
    leaves.every(l => (st[l.key].ex || []).indexOf(p) >= 0));
}
function ppBulkSyncChips() {
  const chips = _('ppBulkChips');
  if (!chips) return;
  const sel = [...ppBulkSel].map(p =>
    '<span class="pr-chip pr-chip-sel" title="' + esc(egLabel(p)) + '"><span>' + esc(egLabel(p)) + '</span>' +
    '<button type="button" onclick="event.stopPropagation();ppBulkPick(decodeURIComponent(this.dataset.pp),false)" data-pp="' + esc(encodeURIComponent(p)) + '" title="' + esc(T('Remove')) + '">×</button></span>');
  // 已全平台排除的代理回显为置灰 chip（pr-chip-allex）：× = 从所有平台批量取消
  const everywhere = ppBulkEverywhere().map(p =>
    '<span class="pr-chip pr-chip-allex" title="' + esc(T('Excluded on every platform — × removes it from all (staged)')) + '"><span>' + esc(egLabel(p)) + '</span>' +
    '<button type="button" onclick="event.stopPropagation();ppBulkRemoveEverywhere(decodeURIComponent(this.dataset.pp))" data-pp="' + esc(encodeURIComponent(p)) + '" title="' + esc(T('Remove')) + '">×</button></span>');
  chips.innerHTML = sel.concat(everywhere).join('');
}
// ppBulkRemoveEverywhere 批量取消：把该代理从所有平台的排除列表移除（暂存），
// 随后重绘各卡片——该代理重新出现在候选菜单与各卡片可选项里，点保存才落盘。
function ppBulkRemoveEverywhere(p) {
  if (!p) return;
  const st = ppRulesState();
  ruleLeaves().forEach(l => {
    const ex = st[l.key] ? st[l.key].ex : null;
    if (!ex) return;
    const i = ex.indexOf(p);
    if (i >= 0) ex.splice(i, 1);
  });
  ppBulkSyncChips();
  renderProxyRules();
  toast(T('Removed from every platform\'s exclusion list (staged) — click Save to apply'), 'success');
}
// ppBulkRenderMenu 候选 = 池内未选中代理（选中项已是 chip，不再进菜单），
// 按 egLabel/原始串包含查询串过滤；已被所有平台排除的代理无可追加，不再展示。
// 行为对齐各平台卡片菜单：无勾选框，整行点击即选中，右侧标注 host。
function ppBulkRenderMenu() {
  const menu = _('ppBulkMenu');
  if (!menu) return;
  const st = ppRulesState();
  const leaves = ruleLeaves();
  const q = ppBulkQuery.toLowerCase();
  const rows = proxyListCache
    .filter(p => p !== EGRESS_DIRECT && !ppBulkSel.has(p))
    .filter(p => !q || egSearchText(p).includes(q))
    .filter(p => !leaves.every(l => (st[l.key].ex || []).indexOf(p) >= 0))
    .map(p => {
      const host = maskProxyLabel(p).replace(/^[a-z0-9]+:\/\//i, '').split(/[\/?#]/)[0];
      return '<div class="pb-row" style="justify-content:space-between" data-bulk="' + esc(p) + '" onclick="ppBulkPick(this.dataset.bulk, true)">' +
        '<span>' + esc(egLabel(p)) + '</span><span class="pr-opt-host">' + esc(host) + '</span></div>';
    }).join('');
  menu.innerHTML = (rows || '<div class="pr-opt-empty">' + T(q ? 'No match' : 'No pool proxies left to exclude') + '</div>') +
    '<div class="pb-foot"><button class="btn btn-sm" style="flex:1" onclick="ppBulkClear()">' + T('Clear selection') + '</button></div>';
}
function ppBulkPick(p, on) {
  if (on) ppBulkSel.add(p); else ppBulkSel.delete(p);
  ppBulkSyncChips();
  ppBulkRenderMenu();
}
function ppBulkClear() {
  ppBulkSel.clear();
  ppBulkSyncChips();
  ppBulkRenderMenu();
}
function ppBulkApply() {
  if (!ppBulkSel.size) { toast(T('Nothing to append'), 'info'); return; }
  const st = ppRulesState();
  let n = 0;
  ppBulkSel.forEach(p => {
    ruleLeaves().forEach(l => {
      if (st[l.key] && st[l.key].ex.indexOf(p) < 0) { st[l.key].ex.push(p); n++; }
    });
  });
  ppBulkSel.clear();
  ppBulkSyncChips();
  pbMenuHide(_('ppBulkMenu'));
  renderProxyRules();
  toast(T('Appended to every platform\'s exclusion list (staged) — click Save to apply'), 'success');
}
// 点击外部收起（菜单挂在 body 下，box 判断用框元素）
document.addEventListener('click', ev => {
  const menu = _('ppBulkMenu');
  if (!pbMenuIsOpen(menu)) return;
  if (menu.contains(ev.target)) return;
  const box = _('ppBulkBox');
  if (box && box.contains(ev.target)) return;
  pbMenuHide(menu);
});

// triggerProxySubUpdate 代理池"立即更新"按钮：立即执行一次订阅刷新任务
// （与 cron 定时同一条链路），结果显示在按钮下方，5s 后自动清除。
async function triggerProxySubUpdate() {
  const btn = _('ppSubUpdateBtn'), el = _('ppSubUpdateResult');
  btn.disabled = true;
  el.textContent = T('Updating...');
  try {
    const r = await api('POST', '/proxypool/subscription/update');
    el.innerHTML = '<span style="color:var(--accent2)">' + esc(r.message || T('Update done')) + '</span>';
  } catch (e) {
    el.innerHTML = '<span style="color:var(--danger)">' + esc(e.message) + '</span>';
  }
  btn.disabled = false;
  setTimeout(() => { el.textContent = ''; }, 8000);
  // 上面的提示只是一行文字，代理列表仍是点击前的旧快照——后端即便已整池替换，
  // 列表不重绘用户就看不到新池（观感像"点了没生效"，只能手动 F5）。这里必须
  // 重绘一次。只重绘代理池区：refreshBindingTables 会丢弃其他标签页的暂存编辑，
  // 而订阅刷新本身不改变绑定，没必要牵连。
  try { await loadProxyPool(); } catch (e) { /* 重绘失败不影响上面的结果提示 */ }
}

// saveGlobalPolicy 统一保存各平台的全局走池开关：一次点击依次下发
// Cline（/config/update）与 OpenCode / WorkBuddy / OpenRouter
// （/opencode/config/update、/openrouter/config/update）。
async function saveGlobalPolicy() {
  try {
    await api('POST', '/config/update', { clineUseProxies: _('ppCline').value === 'true' });
    await api('POST', '/opencode/config/update', { zenUseProxies: _('ppZen').value === 'true', workbuddyUseProxies: _('ppWb').value === 'true' });
    await api('POST', '/trae/config/update', { useProxies: _('ppTrae').value === 'true' });
    await api('POST', '/openrouter/config/update', { useProxies: _('ppOr').value === 'true' });
    await api('POST', '/amd/config/update', { useProxies: _('ppAmd').value === 'true' });
    await api('POST', '/tokenharbor/config/update', { useProxies: _('ppTh').value === 'true' });
    await api('POST', '/other/config/update', { useProxies: _('ppOther').value === 'true' });
    toast('Global policy saved', 'success');
    loadProxyPool();
  } catch (e) { toast(T('Save failed: ') + e.message, 'error'); }
}

// saveIsolationOptions 保存代理隔离区的两项：隔离模式 + 备用代理开关。两字段同在
// zen 配置但分两次请求下发——PROXY_ISOLATION=true 锁定面板时后端会整体拒绝携带
// proxyIsolation 的请求，分开才能让备用代理开关在锁定态照常保存（锁定态下拉框
// disabled，跳过第一笔）。备用代理开关联动绑定表重绘，保存失败回滚勾选。
async function saveIsolationOptions() {
  const iso = _('ppIsolation');
  if (!iso.disabled) {
    try {
      await api('POST', '/opencode/config/update', { proxyIsolation: iso.value === 'true' });
    } catch (e) { toast(T('Save failed: ') + e.message, 'error'); return; }
  }
  const cb = _('ppBackupEnabled');
  try {
    await api('POST', '/opencode/config/update', { backupProxyEnabled: cb.checked });
    backupProxyOn = cb.checked;
    toast('Proxy isolation settings saved', 'success');
    loadProxyPool();
    refreshBindingTables();
  } catch (e) {
    cb.checked = !cb.checked;  // 保存失败回滚勾选
    toast(T('Save failed: ') + e.message, 'error');
  }
}

// refreshBindingTables 按当前 backupProxyOn 重绘绑定表：只刷新已经渲染过行的
// 表（标签页是惰性加载的，没打开过的保持未加载）。行内的暂存改动会被丢弃，
// 与"复位"同语义 —— 开关是全局设置，重绘比就地改 DOM 更不容易出现半更新。
function refreshBindingTables() {
  if (_('accountTableBody') && _('accountTableBody').querySelector('.eg-input')) loadAccounts();
  if (_('ocKeysBody') && _('ocKeysBody').querySelector('.eg-input')) loadOcConfig();
  if (_('wbAccountsBody') && _('wbAccountsBody').querySelector('.eg-input')) loadWorkbuddyAccounts();
  if (_('orKeysBody') && _('orKeysBody').querySelector('.eg-input')) loadOrConfig();
  if (_('amdKeysBody') && _('amdKeysBody').querySelector('.eg-input')) loadAmdConfig();
  if (_('thKeysBody') && _('thKeysBody').querySelector('.eg-input')) loadTHConfig();
  if (_('otherKeysBody') && _('otherKeysBody').querySelector('.eg-input')) loadOtherConfig();
}

// loadOcModels 只维护探测模型下拉（模型列表本体已并入网关设置的可用模型区）。
async function loadOcModels() {
  try {
    const d = await api('GET', '/opencode/models');
    const models = d.data.models || [];
    const sel = _('ocProbeModel');
    if (sel) {
      const prev = sel.value;
      sel.innerHTML = '<option value="">' + T('auto') + '</option>' +
        models.map(m => '<option value="' + esc(m.id) + '">' + esc(m.id) + (m.source === 'live' ? '' : T(' (seed)')) + '</option>').join('');
      if (Array.from(sel.options).some(o => o.value === prev)) sel.value = prev;
    }
  } catch (e) { /* ignore */ }
}

// ========== OpenRouter page ==========
// key 池（oprt: 前缀平台）：配置加载/保存、key 表暂存编辑（绑定 + 路由启用）、
// 批量测试、探测模型下拉、免费模型目录表。协议与 OpenCode 页同构。
let orCfgCache = {};
let orKeySnapshot = {};

async function loadOrConfig() {
  try {
    await loadProxyListCache();
    const d = await api('GET', '/openrouter/config');
    const c = d.data;
    orCfgCache = c;
    const ks = c.keyStates || [];
    const sum = _('orSummary');
    if (sum) {
      const routed = ks.filter(k => k.routingEnabled !== false).length;
      const cooling = ks.filter(k => k.cooling).length;
      sum.textContent = ks.length + ' ' + T('keys') + ' · ' + routed + ' ' + T('active') + ' · ' + cooling + ' ' + T('cooling');
    }
    renderOrKeyStates(ks);
    _('orBaseURL').value = c.baseURL || '';
    _('orMaxConc').value = c.maxConcurrency || 8;
    _('orRetries').value = c.retries || 3;
    // 封禁过滤：黑名单语义是「不再对外提供」，页面模型列表同步剔除。
    await syncModelBlacklist();
    const models = (c.models || []).filter(m => m && !modelBlacklistIds.has(m.id));
    _('orModelsCount').textContent = '(' + models.length + T(' free models') + ')';
    const sel = _('orProbeModel');
    if (sel) {
      const prev = sel.value;
      sel.innerHTML = '<option value="">' + T('auto') + '</option>' +
        models.map(m => '<option value="' + esc(String(m.id || '').replace(/^oprt:/, '')) + '">' + esc(m.id) + '</option>').join('');
      if (Array.from(sel.options).some(o => o.value === prev)) sel.value = prev;
    }
    const tb = _('orModelsBody');
    if (tb) {
      // 展示样式与网关设置页一致：等宽 ID + 免费标签 + 上下文标签（Name 进 title）
      const row = inner => '<div style="display:flex;align-items:center;gap:10px;padding:8px 12px;margin:5px 0;background:rgba(148,163,184,.06);border:1px solid var(--border);border-radius:10px">' + inner + '</div>';
      tb.innerHTML = models.length
        ? models.map(m => row(
            '<span style="font-family:\'JetBrains Mono\',monospace;font-size:13px;flex:1"' + (m.name ? ' title="' + esc(m.name) + '"' : '') + '>' + esc(m.id) + '</span>' +
            '<span class="model-tag free">' + T('no charge') + '</span>' +
            wbContextTagHTML(m.context, T('Context')) +
            '<button class="btn btn-sm btn-danger" data-bl-ban="1" data-platform="openrouter" data-model="' + esc(m.id) + '" data-refresh="or" style="white-space:nowrap">' + T('Ban') + '</button>')).join('')
        : '<div class="empty">' + T('No free models yet — click Refresh models') + '</div>';
    }
  } catch (e) { /* ignore */ }
}

async function saveOrConfig() {
  // key 列表不随本表单提交（增删在 key 表的添加凭据/删除按钮里）
  const body = {
    baseURL: _('orBaseURL').value.trim(),
    maxConcurrency: parseInt(_('orMaxConc').value) || 8,
    retries: parseInt(_('orRetries').value) || 3,
  };
  try {
    await api('POST', '/openrouter/config/update', body);
    toast(T('OpenRouter config saved'), 'success');
    loadOrConfig();
  } catch (e) { toast(T('Save failed: ') + e.message, 'error'); }
}

// refreshWbModels WorkBuddy 目录强制重探：失效 vendor 的 global 缓存与兜底
// 快照（POST models/refresh 即重探一次），随后重拉主目录刷新各页模型块。
async function refreshWbModels() {
  try {
    await api('POST', WB_API_PREFIX + '/models/refresh');
    await loadModels();
    toast(T('Catalog refreshed'), 'success');
  } catch (e) { toast(T('Refresh failed: ') + e.message, 'error'); }
}

async function refreshOrModels() {
  try {
    const d = await api('POST', '/openrouter/models/refresh');
    if (d && d.success === false) { toast(T(d.message || 'Refresh failed: '), 'error'); return; }
    await loadOrConfig();
    toast(T('Catalog refreshed') + ' (' + _('orModelsCount').textContent.replace(/[()]/g, '') + ')', 'success');
    loadOrConfig();
  } catch (e) { toast(T('Refresh failed: ') + e.message, 'error'); }
}

// key 状态表：key 掩码 / 今日累计 token / 最近使用 / 冷却 / 代理绑定 / 测试 /
// 删除 / 启用（暂存编辑）。
function renderOrKeyStates(ks) {
  const tb = _('orKeysBody');
  if (!tb) return;
  orKeySnapshot = {};
  tb.innerHTML = ks.length
    ? ks.map(k => {
        const cool = k.cooling
          ? '<span class="status cooldown"><span class="status-dot cooldown"></span>' + T('Cooldown') + '</span>' +
            (k.cooldownUntil ? '<div style="font-size:10px;color:var(--text3);margin-top:2px;white-space:nowrap" title="' + esc(fmtWhen(k.cooldownUntil)) + '">' + T('Recovers ') + esc(fmtShort(k.cooldownUntil)) + '</div>' : '')
          : '<span class="status active"><span class="status-dot active"></span>' + T('Active') + '</span>';
        const pMain = k.proxyMain || '';
        const pBackup = k.proxyBackup || '';
        const backupLocked = !backupProxyOn || pMain === '' || pMain === EGRESS_DIRECT;
        const backupVal = backupLocked ? pMain : pBackup;
        const isStale = proxyStale;
        const bind = '<td style="white-space:nowrap">' +
          egComboHTML('or', k.index, 'main', pMain, T('Main exit — used whenever available')) + ' ' +
          egComboHTML('or', k.index, 'backup', backupVal, T('Backup exit — used when main is cooling/removed'), backupLocked) +
          (isStale(pMain) || isStale(pBackup) ? ' <span title="' + esc(T('Bound proxy is no longer in the proxy pool — skipped until fixed')) + '" style="color:var(--danger)">⚠</span>' : '') +
          '</td>';
        const routed = k.routingEnabled !== false;
        orKeySnapshot[k.index] = { main: pMain, backup: backupVal, routing: routed };
        const routing = '<td style="text-align:center">' +
          '<input type="checkbox" data-orr="' + k.index + '"' + (routed ? ' checked' : '') + ' title="' + esc(T('Participates in request routing')) + '">' + '</td>';
        const actions = '<td style="white-space:nowrap;text-align:center">' +
          '<button class="btn btn-sm" data-ort="' + k.index + '">Test</button> ' +
          '<button class="btn btn-sm btn-danger" data-ordel="' + k.index + '" title="' + esc(T('Remove this key from the pool')) + '">Delete</button>' + '</td>';
        return '<tr draggable="true" data-drag-id="' + k.index + '"><td>#' + (k.index + 1) + '</td>' +
          '<td style="font-family:monospace;font-size:11px">' + esc(k.keyMask) + '</td>' +
          '<td>' + cool + '</td>' +
          keyTokensCell(k) +
          keyLastUsedCell(k) +
          bind +
          actions +
          routing + '</tr>';
      }).join('')
    : '<tr><td colspan="8" class="empty">No OpenRouter keys configured</td></tr>';
  tb.onclick = e => {
    const t = e.target.closest('button[data-ort]');
    if (t) { testOrKey(parseInt(t.dataset.ort, 10), t); return; }
    const d = e.target.closest('button[data-ordel]');
    if (d) deleteOrKey(parseInt(d.dataset.ordel, 10), d);
  };
  tb.onchange = e => {
    const r = e.target.closest('input[data-orr]');
    if (r) markRowDirty(r.closest('tr'), 'or');
  };
}

function stageAllOrKeysRouting(enabled) {
  document.querySelectorAll('#orKeysBody input[data-orr]').forEach(cb => {
    const snap = orKeySnapshot[parseInt(cb.dataset.orr, 10)];
    if (snap && snap.routing === enabled) return;
    cb.checked = enabled;
    markRowDirty(cb.closest('tr'), 'or');
  });
}
// 清空代理（暂存）：全部 key 清绑定 + 路由全部禁用，与 zen/账号页同语义
function stageClearOrKeyProxies() {
  document.querySelectorAll('#orKeysBody tr').forEach(row => {
    const main = row.querySelector('.eg-input[data-eg="or"][data-slot="main"]');
    if (!main) return;
    let dirty = stageRowBindingClear(row, 'or');
    const cb = row.querySelector('input[data-orr]');
    if (cb && cb.checked) { cb.checked = false; dirty = true; }
    if (dirty) markRowDirty(row, 'or');
  });
}
async function saveOrKeyEdits() {
  const rows = [...document.querySelectorAll('#orKeysBody tr')].filter(r => r.querySelector('.eg-input'));
  let changed = 0, failed = 0;
  for (const row of rows) {
    const mainInput = row.querySelector('.eg-input[data-eg="or"][data-slot="main"]');
    const idx = parseInt(mainInput.dataset.egid, 10);
    const main = mainInput.dataset.value || '';
    const backup = row.querySelector('.eg-input[data-eg="or"][data-slot="backup"]').dataset.value || '';
    const rcb = row.querySelector('input[data-orr]');
    const snap = orKeySnapshot[idx] || { main: '', backup: '', routing: true };
    try {
      if (main !== snap.main || backup !== snap.backup) {
        await api('POST', '/openrouter/keys/proxy', { index: idx, main: main, backup: backup });
      }
      if (rcb && rcb.checked !== snap.routing) {
        await api('POST', '/openrouter/keys/routing', { index: idx, enabled: rcb.checked });
      }
      if (main !== snap.main || backup !== snap.backup || (rcb && rcb.checked !== snap.routing)) changed++;
      row.classList.remove('tr-dirty');
    } catch (e) { failed++; }
  }
  clearDirtyMarks('orKeysBody', 'orKeysSaveBtn');
  if (failed) toast(T('Save failed for ') + failed + T(' account(s)'), 'error');
  else if (!changed) toast(T('No changes to save'), 'info');
  else toast(T('Saved ') + changed + T(' account change(s)'), 'success');
  loadOrConfig(); loadStats();
}

function resetOrKeyEdits() {
  clearDirtyMarks('orKeysBody', 'orKeysSaveBtn');
  loadOrConfig();
}

async function deleteOrKey(index, btn) {
  if (!confirm(T('Delete this key?'))) return;
  const original = btn ? btn.innerHTML : '';
  if (btn) btn.disabled = true;
  try {
    await api('POST', '/openrouter/keys/delete', { index: index });
    toast(T('Key #') + (index + 1) + T(' (deleted)'), 'success');
  } catch (e) {
    toast(T('Delete failed: ') + e.message, 'error');
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = original; }
    loadOrConfig(); loadStats();
  }
}

async function testOrKey(index, btn) {
  const original = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span>Testing'; }
  try {
    const d = await api('POST', '/openrouter/keys/test', {
      index: index,
      model: (_('orProbeModel') ? _('orProbeModel').value : '')
    });
    const r = d.data || {};
    const label = { active: T('OK'), cooldown: T('Cooldown'), error: T('Error') }[r.status] || r.status;
    let msg = T('Key #') + (index + 1) + T(' (') + (r.keyMask || '') + T(') — ') + label;
    if (r.model) msg += T(' via ') + r.model;
    if (r.latencyMs != null) msg += ' (' + r.latencyMs + 'ms)';
    if (r.status === 'active') msg += T(' — cooldown cleared');
    if (r.cooldownUntil) msg += T('\nEstimated recovery: ') + fmtWhen(r.cooldownUntil) + (r.remaining ? T(' (remaining ') + r.remaining + T(')') : '');
    if (r.reason && r.reason !== 'ok') msg += '\n' + r.reason;
    const type = r.status === 'active' ? 'success' : (r.status === 'cooldown' ? 'warning' : 'error');
    toast(msg, type, 6000);
  } catch (e) {
    toast(T('Test failed: ') + e.message, 'error');
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = original; }
    loadOrConfig();
  }
}

async function batchTestOrKeys() {
  const btn = _('orTestBtn');
  const idxs = Object.keys(orKeySnapshot).map(Number).sort((a, b) => a - b);
  if (!idxs.length) { toast(T('No keys to test'), 'info'); return; }
  const original = btn ? btn.innerHTML : '';
  if (btn) btn.disabled = true;
  let ok = 0, cool = 0, err = 0;
  try {
    for (let i = 0; i < idxs.length; i++) {
      if (btn) btn.innerHTML = '<span class="loading"></span>' + (i + 1) + '/' + idxs.length;
      try {
        const d = await api('POST', '/openrouter/keys/test', {
          index: idxs[i],
          model: (_('orProbeModel') ? _('orProbeModel').value : '')
        });
        const r = d.data || {};
        if (r.status === 'active') ok++;
        else if (r.status === 'cooldown') cool++;
        else err++;
      } catch (e) { err++; }
    }
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = original; }
  }
  toast(T('Batch test done: ') + idxs.length + T(' tested, ') + ok + T(' ok') +
    (cool ? T(', ') + cool + T(' cooling') : '') + (err ? T(', ') + err + T(' error') : ''),
    err ? 'warning' : 'success', 6000);
  loadOrConfig();
}

function openOrKeyAddDialog() {
  _('orKeyAddInput').value = '';
  _('orKeyAddDialog').showModal();
}
function closeOrKeyAddDialog() { _('orKeyAddDialog').close(); }
async function orKeyAddFromDialog() {
  const lines = _('orKeyAddInput').value.split('\n').map(s => s.trim()).filter(Boolean);
  if (!lines.length) { toast(T('Enter at least one key'), 'error'); return; }
  const cur = (orCfgCache.keys && orCfgCache.keys.length) ? orCfgCache.keys : [];
  const fresh = lines.filter(l => !cur.includes(l));
  if (!fresh.length) { toast(T('No new keys (all already in the pool)'), 'info'); return; }
  const btn = _('orKeyAddSubmit');
  btn.disabled = true;
  try {
    await api('POST', '/openrouter/config/update', { keys: cur.concat(fresh) });
    toast(T('Added ') + fresh.length + T(' key(s)'), 'success');
    closeOrKeyAddDialog();
    loadOrConfig(); loadStats();
  } catch (e) {
    toast(T('Save failed: ') + e.message, 'error');
  } finally {
    btn.disabled = false;
  }
}

// ========== Trae page ==========
// Trae 账号池（trae: 前缀平台，Cloud-IDE-JWT 凭据）：配置加载/保存、账号表
// 暂存编辑（绑定 + 路由启用 + 昵称改名）、批量测试、模型目录。协议与
// OpenRouter 页同构，凭据列展示 JWT 掩码 + 昵称（铅笔改名）。
let traeCfgCache = {};
let traeAccSnapshot = {};

async function loadTraeConfig() {
  try {
    await loadProxyListCache();
    const d = await api('GET', '/trae/config');
    const c = d.data;
    traeCfgCache = c;
    const accs = c.accounts || [];
    const sum = _('traeSummary');
    if (sum) {
      const routed = accs.filter(a => a.routingEnabled !== false).length;
      const cooling = accs.filter(a => a.cooling).length;
      sum.textContent = accs.length + ' ' + T('accounts') + ' · ' + routed + ' ' + T('active') + ' · ' + cooling + ' ' + T('cooling');
    }
    renderTraeAccounts(accs);
    _('traeGateway').value = c.gateway || '';
    if (_('traeEndpoint')) _('traeEndpoint').value = c.endpoint || 'auto';
    _('traeMaxConc').value = c.maxConcurrency || 8;
    _('traeRetries').value = c.retries || 3;
    if (_('traeSelection')) _('traeSelection').value = c.selection || '';
    if (_('traeAutoCheckin')) _('traeAutoCheckin').checked = c.autoCheckin === true;
    // 封禁过滤：黑名单语义是「不再对外提供」，页面模型列表同步剔除。
    await syncModelBlacklist();
    const models = (c.models || []).filter(m => m && !modelBlacklistIds.has(m.id));
    _('traeModelsCount').textContent = '(' + models.length + T(' models') + ')';
    const sel = _('traeProbeModel');
    if (sel) {
      const prev = sel.value;
      sel.innerHTML = '<option value="">' + T('auto') + '</option>' +
        models.map(m => '<option value="' + esc(String(m.id || '').replace(/^trae:/, '')) + '">' + esc(m.id) + '</option>').join('');
      if (Array.from(sel.options).some(o => o.value === prev)) sel.value = prev;
    }
    const tb = _('traeModelsBody');
    if (tb) {
      const row = inner => '<div style="display:flex;align-items:center;gap:10px;padding:8px 12px;margin:5px 0;background:rgba(148,163,184,.06);border:1px solid var(--border);border-radius:10px">' + inner + '</div>';
      tb.innerHTML = models.length
        ? models.map(m => row(
            '<span style="font-family:\'JetBrains Mono\',monospace;font-size:13px;flex:1"' + (m.name ? ' title="' + esc(m.name) + '"' : '') + '>' + esc(m.id) + '</span>' +
            '<span class="model-tag free">' + T('no charge') + '</span>' +
            wbContextTagHTML(m.context, T('Context')) +
            '<button class="btn btn-sm btn-danger" data-bl-ban="1" data-platform="trae" data-model="' + esc(m.id) + '" data-scope="' + esc(traeCfgCache.endpoint || 'auto') + '" data-refresh="trae" style="white-space:nowrap">' + T('Ban') + '</button>')).join('')
        : '<div class="empty">' + T((c.accounts || []).some(x => x.accountEnabled !== false)
            ? 'No models yet — click Refresh models'
            : 'Models appear here once an account is enabled — the catalog syncs from upstream') + '</div>';
    }
  } catch (e) { /* ignore */ }
}

async function saveTraeConfig() {
  const body = {
    gateway: _('traeGateway').value.trim(),
    maxConcurrency: parseInt(_('traeMaxConc').value) || 8,
    retries: parseInt(_('traeRetries').value) || 3,
    selection: _('traeSelection') ? _('traeSelection').value : '',
    autoCheckin: !(!_('traeAutoCheckin') || !_('traeAutoCheckin').checked),
  };
  try {
    await api('POST', '/trae/config/update', body);
    toast(T('TraeWork config saved'), 'success');
    loadTraeConfig();
  } catch (e) { toast(T('Save failed: ') + e.message, 'error'); }
}

async function saveTraeEndpoint(ep) {
  try {
    await api('POST', '/trae/config/update', { endpoint: ep });
    toast(T('Upstream endpoint switched'), 'success');
    loadTraeConfig();
  } catch (e) {
    toast(T('Save failed: ') + e.message, 'error');
    loadTraeConfig(); // 回读真实值，避免下拉框与后端漂移
  }
}

async function refreshTraeModels() {
  try {
    const d = await api('POST', '/trae/models/refresh');
    // 后端对无启用账号返回 success:false + Message（api() 不抛异常），按统一口径提示
    if (d && d.success === false) { toast(T(d.message || 'Refresh failed: '), 'error'); return; }
    await loadTraeConfig();
    toast(T('Catalog refreshed') + ' (' + _('traeModelsCount').textContent.replace(/[()]/g, '') + ')', 'success');
  } catch (e) { toast(T('Refresh failed: ') + e.message, 'error'); }
}

// 账号状态表：昵称 + JWT 掩码 / token 到期 / 今日累计 token / 额度 / 签到 /
// 冷却 / 代理绑定 / 测试 / 删除 / 双开关（反代 = 路由参与，账号 = 总闸，
// 对齐 WorkBuddy：总闸关 = 签到/额度刷新等维护任务一并停）。
function renderTraeAccounts(accs) {
  const tb = _('traeAccountsBody');
  if (!tb) return;
  traeAccSnapshot = {};
  tb.innerHTML = accs.length
    ? accs.map(a => {
        const cool = a.cooling
          ? '<span class="status cooldown"><span class="status-dot cooldown"></span>' + T('Cooldown') + '</span>' +
            (a.cooldownUntil ? '<div style="font-size:10px;color:var(--text3);margin-top:2px;white-space:nowrap" title="' + esc(fmtWhen(a.cooldownUntil)) + '">' + T('Recovers ') + esc(fmtShort(a.cooldownUntil)) + '</div>' : '')
          : '<span class="status active"><span class="status-dot active"></span>' + T('Active') + '</span>';
        // token 有效期：状态列内另起一行（绿色正常 / 红色过期 / 灰色无刷新令牌）
        let tokenState = '';
        if (a.expiredAt) {
          const cls = a.tokenUsable === false ? 'var(--danger)' : 'var(--accent2)';
          const txt = a.tokenUsable === false ? T('Token expired') + ' ' : '';
          tokenState = '<div style="font-size:11px;margin-top:2px;color:' + cls + '">' + txt + esc(fmtShort(a.expiredAt)) + '</div>';
        } else if (!a.hasRefresh) {
          tokenState = '<div style="font-size:11px;margin-top:2px;color:var(--text3)">' + T('No refresh token') + '</div>';
        }
        let checkinV = '';
        if (a.checkin && a.checkin.date === new Date().toLocaleDateString('sv')) {
          const ok = a.checkin.checkedIn;
          const txt = ok ? T('Checked in') : T('Check-in failed');
          const cls = ok ? 'status active' : 'status cooldown';
          checkinV = '<div class="' + cls + '" style="writing-mode:vertical-rl;text-orientation:upright;letter-spacing:2px;font-size:10px;padding:5px 2px;border-radius:8px;align-self:stretch;display:flex;align-items:center;justify-content:center" title="' + esc(a.checkin.lastError || txt) + '">' + esc(txt) + '</div>';
        }
        const label = a.label || ('Account ' + (a.index + 1));
        const shortId = a.id.length > 10 ? a.id.slice(0, 10) + '…' : a.id;
        const pMain = a.proxyMain || '';
        const pBackup = a.proxyBackup || '';
        const backupLocked = !backupProxyOn || pMain === '' || pMain === EGRESS_DIRECT;
        const backupVal = backupLocked ? pMain : pBackup;
        const isStale = proxyStale;
        const bind = '<td style="white-space:nowrap">' +
          egComboHTML('trae', a.index, 'main', pMain, T('Main exit — used whenever available')) + ' ' +
          egComboHTML('trae', a.index, 'backup', backupVal, T('Backup exit — used when main is cooling/removed'), backupLocked) +
          (isStale(pMain) || isStale(pBackup) ? ' <span title="' + esc(T('Bound proxy is no longer in the proxy pool — skipped until fixed')) + '" style="color:var(--danger)">⚠</span>' : '') +
          '</td>';
        let credits = '-';
        if (a.credits) {
          if (a.credits.unlimited) credits = T('Unlimited');
          else if (a.credits.limit > 0) credits = fmtTokens(Math.max(0, Math.round(a.credits.remaining))) + ' / ' + fmtTokens(Math.round(a.credits.limit));
          else if (a.credits.remaining > 0) credits = fmtTokens(Math.round(a.credits.remaining));
        }
        // 双开关快照：routing = 原始路由意愿（不含总闸与运算），account = 总闸
        const accOn = a.accountEnabled !== false;
        const routedIntent = a.routingEnabled !== false;
        traeAccSnapshot[a.index] = { main: pMain, backup: backupVal, routing: routedIntent, account: accOn };
        // 反代开关显示态 = 总闸开 && 路由意愿开；总闸关时反代禁用（wb 同款互锁）
        const poolCell = '<td style="text-align:center">' +
          '<input type="checkbox" data-trr="' + a.index + '"' + (accOn && routedIntent ? ' checked' : '') + (!accOn ? ' disabled' : '') +
          ' title="' + esc(T('Gateway (reverse proxy) switch: unchecked = model sessions never route to this account; check-in and all other maintenance tasks are unaffected. Can only be ticked while the account switch is on.')) + '"></td>';
        const accCell = '<td style="text-align:center">' +
          '<input type="checkbox" data-tracc="' + a.index + '"' + (accOn ? ' checked' : '') +
          ' title="' + esc(T('Account master switch. Off = the account does not exist: check-in, balance refresh and Test all skip it, and it never routes.')) + '"></td>';
        const actions = '<td style="white-space:nowrap;text-align:center">' +
          '<button class="btn btn-sm" data-trt="' + a.index + '">Test</button> ' +
          '<button class="btn btn-sm btn-danger" data-trdel="' + a.index + '" title="Remove this account from the pool">Delete</button>' + '</td>';
        return '<tr draggable="true" data-drag-id="' + a.index + '"><td>#' + (a.index + 1) + '</td>' +
          '<td style="min-width:0"><div style="max-width:150px" title="' + esc(label + '\n' + a.id + '\n' + (a.tokenMask || '') + (a.hasRefresh ? '' : '\n' + T('No refresh token'))) + '">' +
            '<div class="nick-wrap">' +
              '<span class="nick-name" style="font-weight:600">' + esc(label) + '</span>' +
              nickPen(a.index, label, T('Edit nickname')) +
            '</div>' +
            '<div class="wb-account-id">' + esc(shortId) + '</div>' +
          '</div></td>' +
          '<td><div style="display:flex;gap:6px;align-items:center">' +
            '<div style="display:flex;flex-direction:column;gap:2px;min-width:0">' + cool + (tokenState ? tokenState : '') + '</div>' +
            checkinV +
          '</div></td>' +
          '<td>' + credits + '</td>' +
          keyTokensCell(a) +
          keyLastUsedCell(a) +
          bind +
          actions +
          poolCell +
          accCell + '</tr>';
      }).join('')
    : '<tr><td colspan="9" class="empty">' + T('No TraeWork accounts yet — click Add accounts') + '</td></tr>';
  tb.onclick = e => {
    const pen = e.target.closest('.nick-pen');
    if (pen) { startNickEdit(pen, { save: saveTraeLabel, reload: loadTraeConfig, allowEmpty: true }); return; }
    const t = e.target.closest('button[data-trt]');
    if (t) { testTraeAccount(parseInt(t.dataset.trt, 10), t); return; }
    const d = e.target.closest('button[data-trdel]');
    if (d) deleteTraeAccount(parseInt(d.dataset.trdel, 10), d);
  };
  tb.onchange = e => {
    const r = e.target.closest('input[data-trr]');
    if (r) { markRowDirty(r.closest('tr'), 'trae'); return; }
    const acc = e.target.closest('input[data-tracc]');
    if (acc) {
      // 总闸联动：关闭 → 同行反代开关禁用并取消勾选；重开 → 恢复快照里的
      // 路由意愿（持久值未动，重开即恢复路由意愿）。两者都进暂存。
      const row = acc.closest('tr');
      const poolCb = row.querySelector('input[data-trr]');
      const snap = traeAccSnapshot[parseInt(acc.dataset.tracc, 10)] || {};
      if (!acc.checked) {
        if (poolCb) { poolCb.disabled = true; poolCb.checked = false; }
      } else if (poolCb) {
        poolCb.disabled = false;
        poolCb.checked = snap.routing !== false;
      }
      markRowDirty(row, 'trae');
    }
  };
}

async function saveTraeLabel(index, label) {
  // data-nick-id 走 dataset 是字符串，端点 index 字段要 int
  await api('POST', '/trae/accounts/label', { index: parseInt(index, 10), label: label });
}

function stageAllTraeRouting(enabled) {
  document.querySelectorAll('#traeAccountsBody input[data-trr]').forEach(cb => {
    // wb 同款互锁：总闸关闭（禁用态）的行不参与批量启停
    if (cb.disabled) return;
    const snap = traeAccSnapshot[parseInt(cb.dataset.trr, 10)];
    if (snap && snap.routing === enabled) return;
    cb.checked = enabled;
    markRowDirty(cb.closest('tr'), 'trae');
  });
}
// 清空代理（暂存）：全部账号清绑定 + 路由全部禁用，与 zen/账号页同语义
function stageClearTraeProxies() {
  document.querySelectorAll('#traeAccountsBody tr').forEach(row => {
    const main = row.querySelector('.eg-input[data-eg="trae"][data-slot="main"]');
    if (!main) return;
    let dirty = stageRowBindingClear(row, 'trae');
    const cb = row.querySelector('input[data-trr]');
    if (cb && cb.checked) { cb.checked = false; dirty = true; }
    if (dirty) markRowDirty(row, 'trae');
  });
}
async function saveTraeEdits() {
  const rows = [...document.querySelectorAll('#traeAccountsBody tr')].filter(r => r.querySelector('.eg-input'));
  let changed = 0, failed = 0;
  for (const row of rows) {
    const mainInput = row.querySelector('.eg-input[data-eg="trae"][data-slot="main"]');
    const idx = parseInt(mainInput.dataset.egid, 10);
    const main = mainInput.dataset.value || '';
    const backup = row.querySelector('.eg-input[data-eg="trae"][data-slot="backup"]').dataset.value || '';
    const rcb = row.querySelector('input[data-trr]');
    const accCb = row.querySelector('input[data-tracc]');
    const snap = traeAccSnapshot[idx] || { main: '', backup: '', routing: true, account: true };
    // wb 同款目标态：总闸关闭时不提交路由变更（重开自动恢复路由意愿）
    const nextAccount = accCb ? accCb.checked : snap.account;
    const nextRouting = (nextAccount && rcb) ? rcb.checked : snap.routing;
    try {
      if (main !== snap.main || backup !== snap.backup) {
        await api('POST', '/trae/accounts/proxy', { index: idx, main: main, backup: backup });
      }
      if (nextAccount !== snap.account) {
        await api('POST', '/trae/accounts/enabled', { index: idx, enabled: nextAccount });
      }
      if (nextRouting !== snap.routing) {
        await api('POST', '/trae/accounts/routing', { index: idx, enabled: nextRouting });
      }
      if (main !== snap.main || backup !== snap.backup || nextAccount !== snap.account || nextRouting !== snap.routing) changed++;
      row.classList.remove('tr-dirty');
    } catch (e) { failed++; }
  }
  clearDirtyMarks('traeAccountsBody', 'traeSaveBtn');
  if (failed) toast(T('Save failed for ') + failed + T(' account(s)'), 'error');
  else if (!changed) toast(T('No changes to save'), 'info');
  else toast(T('Saved ') + changed + T(' account change(s)'), 'success');
  loadTraeConfig(); loadStats();
}

function resetTraeEdits() {
  clearDirtyMarks('traeAccountsBody', 'traeSaveBtn');
  loadTraeConfig();
}

async function deleteTraeAccount(index, btn) {
  if (!confirm(T('Delete this account?'))) return;
  const original = btn ? btn.innerHTML : '';
  if (btn) btn.disabled = true;
  try {
    await api('POST', '/trae/accounts/delete', { index: index });
    toast(T('Account #') + (index + 1) + T(' (deleted)'), 'success');
  } catch (e) {
    toast(T('Delete failed: ') + e.message, 'error');
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = original; }
    loadTraeConfig(); loadStats();
  }
}

async function testTraeAccount(index, btn) {
  const original = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span>Testing'; }
  try {
    const d = await api('POST', '/trae/accounts/test', { index: index });
    const r = d.data || {};
    const label = { active: T('OK'), cooldown: T('Cooldown'), error: T('Error') }[r.status] || r.status;
    let msg = T('Account #') + (index + 1) + (r.screenName ? ' (' + r.screenName + ')' : '') + T(' — ') + label;
    if (r.latencyMs != null) msg += ' (' + r.latencyMs + 'ms)';
    if (r.credits) {
      msg += r.credits.unlimited ? T(' — unlimited credits') : T(' — credits: ') + (r.credits.remaining != null ? Math.round(r.credits.remaining) : '?');
      if (r.credits.identity) msg += ' (' + r.credits.identity + ')';
    }
    if (r.status === 'active') msg += T(' — cooldown cleared');
    if (r.cooldownUntil) msg += T('\nEstimated recovery: ') + fmtWhen(r.cooldownUntil) + (r.remaining ? T(' (remaining ') + r.remaining + T(')') : '');
    if (r.reason && r.reason !== 'ok') msg += '\n' + r.reason;
    const type = r.status === 'active' ? 'success' : (r.status === 'cooldown' ? 'warning' : 'error');
    toast(msg, type, 6000);
  } catch (e) {
    toast(T('Test failed: ') + e.message, 'error');
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = original; }
    loadTraeConfig();
  }
}

async function batchTestTraeAccounts() {
  const btn = _('traeTestBtn');
  const idxs = Object.keys(traeAccSnapshot).map(Number).sort((a, b) => a - b);
  if (!idxs.length) { toast(T('No accounts to test'), 'info'); return; }
  const original = btn ? btn.innerHTML : '';
  if (btn) btn.disabled = true;
  let ok = 0, cool = 0, err = 0;
  try {
    for (let i = 0; i < idxs.length; i++) {
      if (btn) btn.innerHTML = '<span class="loading"></span>' + (i + 1) + '/' + idxs.length;
      try {
        const d = await api('POST', '/trae/accounts/test', { index: idxs[i] });
        const r = d.data || {};
        if (r.status === 'active') ok++;
        else if (r.status === 'cooldown') cool++;
        else err++;
      } catch (e) { err++; }
    }
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = original; }
  }
  toast(T('Batch test done: ') + idxs.length + T(' tested, ') + ok + T(' ok') +
    (cool ? T(', ') + cool + T(' cooling') : '') + (err ? T(', ') + err + T(' error') : ''),
    err ? 'warning' : 'success', 6000);
  loadTraeConfig();
}

['traeAccWeb', 'traeAccManual'].forEach(id => {
  const d = _(id);
  if (d) d.addEventListener('toggle', () => {
    if (d.open) ['traeAccWeb', 'traeAccManual'].forEach(o => { if (o !== id && _(o)) _(o).open = false; });
  });
});
function openTraeAddDialog() {
  _('traeAddInput').value = '';
  _('traeHelperStatus').textContent = T('Checking local helper…');
  // 手风琴回到默认态（details 状态跨弹窗开合保留，需显式复位）
  if (_('traeAccWeb')) _('traeAccWeb').open = true;
  if (_('traeAccManual')) _('traeAccManual').open = false;
  _('traeAddDialog').showModal();
  traeHelperSince = new Date().toISOString();
  if (traeHelperTimer) clearInterval(traeHelperTimer);
  probeTraeHelper();
  traeHelperTimer = setInterval(probeTraeHelper, 2500);
  pollTraeHelperStatus();
}
function closeTraeAddDialog() {
  if (traeHelperTimer) { clearInterval(traeHelperTimer); traeHelperTimer = null; }
  _('traeAddDialog').close();
}

// ========== Trae 本机授权助手（对齐上游原版 web_login.py 架构）==========
// 授权页强依赖本机 helper（127.0.0.1:8765）：面板探测 /healthz 判定助手
// 在跑；授权完成后 helper 把凭据 POST 到网关 /api/web-auth 入库，面板轮询
// 最近入库状态展示结果。浏览器需已登录 trae.cn。
let traeHelperTimer = null;
let traeHelperSince = '';

function probeTraeHelper() {
  fetch('http://127.0.0.1:8765/healthz', { cache: 'no-store' }).then(r => r.json()).then(h => {
    const relay = (h && h.relay || '').replace(/\/$/, '');
    if (relay && relay !== location.origin) {
      _('traeHelperStatus').innerHTML = esc(T('Helper detected, but it writes credentials to ANOTHER deployment: ')) +
        '<b>' + esc(relay) + '</b>' + esc(T(' — re-download it from THIS panel and restart it (python web_login.py)'));
      return;
    }
    _('traeHelperStatus').textContent = T('Local helper detected (writes to this gateway) — click Open helper page, then use the authorization button there');
  }).catch(() => {
    _('traeHelperStatus').textContent = T('Local helper NOT detected — download it, run "python web_login.py", then retry');
  });
}
function openTraeHelperPage() {
  window.open('http://127.0.0.1:8765/', 'trae-oauth-helper', 'width=560,height=760');
}
function pollTraeHelperStatus() {
  api('GET', '/trae/helper/status').then(d => {
    const r = d.data || {};
    if (!r.seen || !r.at || r.at < traeHelperSince) return;
    _('traeHelperStatus').textContent = (r.success ? T('Account ready: ') : T('Authorization failed: ')) + (r.message || '');
    if (r.success) {
      toast(r.added ? T('Account added: ') + (r.label || '') : T('Account already in the pool: ') + (r.label || ''), 'success');
      traeHelperSince = r.at;
      loadTraeConfig(); loadStats();
    }
  }).catch(() => { /* 轮询失败静默（网关短暂不可达时下轮再试） */ });
}

// ========== Trae 签到与余额 ==========

async function refreshTraeCredits() {
  await api('POST', '/trae/credits/refresh');
  toast(T('Balances refreshed'), 'success');
  loadTraeConfig();
}
async function runTraeCheckin() {
  const d = await api('POST', '/trae/checkin');
  const r = d.data || {};
  let msg = T('Check-in done: ') + (r.ok || 0) + '/' + (r.total || 0);
  if (r.already) msg += T(', ') + r.already + T(' already');
  if (r.failed) msg += T(', ') + r.failed + T(' failed');
  toast(msg, r.failed ? 'warning' : 'success', 6000);
  loadTraeConfig();
}
async function traeAddFromDialog() {
  const lines = _('traeAddInput').value.split('\n').map(s => s.trim()).filter(Boolean);
  if (!lines.length) { toast(T('Enter at least one token'), 'error'); return; }
  const btn = _('traeAddSubmit');
  btn.disabled = true;
  try {
    const d = await api('POST', '/trae/accounts/add', { tokens: lines });
    const r = d.data || {};
    let msg = T('Added ') + (r.added || 0) + T(' account(s)');
    if (r.skipped) msg += T(', skipped ') + r.skipped;
    toast(msg, r.added ? 'success' : 'warning');
    closeTraeAddDialog();
    loadTraeConfig(); loadStats();
  } catch (e) {
    toast(T('Save failed: ') + e.message, 'error');
  } finally {
    btn.disabled = false;
  }
}

// ========== AMDCloud page ==========
// AMDCloud key 池（amd: 前缀平台）：协议与 OpenRouter 页同构。
let amdCfgCache = {};
let amdKeySnapshot = {};

async function loadAmdConfig() {
  try {
    await loadProxyListCache();
    const d = await api('GET', '/amd/config');
    const c = d.data;
    amdCfgCache = c;
    const ks = c.keyStates || [];
    const sum = _('amdSummary');
    if (sum) {
      const routed = ks.filter(k => k.routingEnabled !== false).length;
      const cooling = ks.filter(k => k.cooling).length;
      sum.textContent = ks.length + ' ' + T('keys') + ' · ' + routed + ' ' + T('active') + ' · ' + cooling + ' ' + T('cooling');
    }
    renderAmdKeyStates(ks);
    _('amdBaseURL').value = c.baseURL || '';
    _('amdMaxConc').value = c.maxConcurrency || 8;
    _('amdRetries').value = c.retries || 3;
    // 封禁过滤：黑名单语义是「不再对外提供」，页面模型列表同步剔除。
    await syncModelBlacklist();
    const models = (c.models || []).filter(m => m && !modelBlacklistIds.has(m.id));
    _('amdModelsCount').textContent = '(' + models.length + T(' free models') + ')';
    const sel = _('amdProbeModel');
    if (sel) {
      const prev = sel.value;
      sel.innerHTML = '<option value="">' + T('auto') + '</option>' +
        models.map(m => '<option value="' + esc(String(m.id || '').replace(/^amd:/, '')) + '">' + esc(m.id) + '</option>').join('');
      if (Array.from(sel.options).some(o => o.value === prev)) sel.value = prev;
    }
    const tb = _('amdModelsBody');
    if (tb) {
      // 展示样式与网关设置页一致：等宽 ID + 免费标签 + 上下文标签（Name 进 title）
      const row = inner => '<div style="display:flex;align-items:center;gap:10px;padding:8px 12px;margin:5px 0;background:rgba(148,163,184,.06);border:1px solid var(--border);border-radius:10px">' + inner + '</div>';
      tb.innerHTML = models.length
        ? models.map(m => row(
            '<span style="font-family:\'JetBrains Mono\',monospace;font-size:13px;flex:1"' + (m.name ? ' title="' + esc(m.name) + '"' : '') + '>' + esc(m.id) + '</span>' +
            '<span class="model-tag free">' + T('no charge') + '</span>' +
            wbContextTagHTML(m.context, T('Context')) +
            '<button class="btn btn-sm btn-danger" data-bl-ban="1" data-platform="amd" data-model="' + esc(m.id) + '" data-refresh="amd" style="white-space:nowrap">' + T('Ban') + '</button>')).join('')
        : '<div class="empty">' + T((ks || []).some(k => k.routingEnabled !== false)
            ? 'No models yet — click Refresh models'
            : 'Models appear here once a credential is enabled — the catalog syncs from upstream') + '</div>';
    }
  } catch (e) { /* ignore */ }
}

async function saveAmdConfig() {
  // key 列表不随本表单提交（增删在 key 表的添加凭据/删除按钮里）
  const body = {
    baseURL: _('amdBaseURL').value.trim(),
    maxConcurrency: parseInt(_('amdMaxConc').value) || 8,
    retries: parseInt(_('amdRetries').value) || 3
  };
  try {
    await api('POST', '/amd/config/update', body);
    toast(T('AMDCloud config saved'), 'success');
    loadAmdConfig();
  } catch (e) { toast(T('Save failed: ') + e.message, 'error'); }
}

async function refreshAmdModels() {
  try {
    const d = await api('POST', '/amd/models/refresh');
    // 后端对无启用 key 返回 success:false + Message（api() 不抛异常），按统一口径提示
    if (d && d.success === false) { toast(T(d.message || 'Refresh failed: '), 'error'); return; }
    await loadAmdConfig();
    toast(T('Catalog refreshed') + ' (' + _('amdModelsCount').textContent.replace(/[()]/g, '') + ')', 'success');
    loadAmdConfig();
  } catch (e) { toast(T('Refresh failed: ') + e.message, 'error'); }
}

// key 状态表：key 掩码 / 今日累计 token / 最近使用 / 冷却 / 代理绑定 / 测试 /
// 删除 / 启用（暂存编辑）。
function renderAmdKeyStates(ks) {
  const tb = _('amdKeysBody');
  if (!tb) return;
  amdKeySnapshot = {};
  tb.innerHTML = ks.length
    ? ks.map(k => {
        const cool = k.cooling
          ? '<span class="status cooldown"><span class="status-dot cooldown"></span>' + T('Cooldown') + '</span>' +
            (k.cooldownUntil ? '<div style="font-size:10px;color:var(--text3);margin-top:2px;white-space:nowrap" title="' + esc(fmtWhen(k.cooldownUntil)) + '">' + T('Recovers ') + esc(fmtShort(k.cooldownUntil)) + '</div>' : '')
          : '<span class="status active"><span class="status-dot active"></span>' + T('Active') + '</span>';
        const pMain = k.proxyMain || '';
        const pBackup = k.proxyBackup || '';
        const backupLocked = !backupProxyOn || pMain === '' || pMain === EGRESS_DIRECT;
        const backupVal = backupLocked ? pMain : pBackup;
        const isStale = proxyStale;
        const bind = '<td style="white-space:nowrap">' +
          egComboHTML('amd', k.index, 'main', pMain, T('Main exit — used whenever available')) + ' ' +
          egComboHTML('amd', k.index, 'backup', backupVal, T('Backup exit — used when main is cooling/removed'), backupLocked) +
          (isStale(pMain) || isStale(pBackup) ? ' <span title="' + esc(T('Bound proxy is no longer in the proxy pool — skipped until fixed')) + '" style="color:var(--danger)">⚠</span>' : '') +
          '</td>';
        const routed = k.routingEnabled !== false;
        amdKeySnapshot[k.index] = { main: pMain, backup: backupVal, routing: routed };
        const routing = '<td style="text-align:center">' +
          '<input type="checkbox" data-amdr="' + k.index + '"' + (routed ? ' checked' : '') + ' title="' + esc(T('Participates in request routing')) + '">' + '</td>';
        const actions = '<td style="white-space:nowrap;text-align:center">' +
          '<button class="btn btn-sm" data-amdt="' + k.index + '">Test</button> ' +
          '<button class="btn btn-sm btn-danger" data-amddel="' + k.index + '" title="' + esc(T('Remove this key from the pool')) + '">Delete</button>' + '</td>';
        return '<tr draggable="true" data-drag-id="' + k.index + '"><td>#' + (k.index + 1) + '</td>' +
          '<td style="font-family:monospace;font-size:11px">' + esc(k.keyMask) + '</td>' +
          '<td>' + cool + '</td>' +
          keyTokensCell(k) +
          keyLastUsedCell(k) +
          bind +
          actions +
          routing + '</tr>';
      }).join('')
    : '<tr><td colspan="8" class="empty">No AMDCloud keys configured</td></tr>';
  tb.onclick = e => {
    const t = e.target.closest('button[data-amdt]');
    if (t) { testAmdKey(parseInt(t.dataset.amdt, 10), t); return; }
    const d = e.target.closest('button[data-amddel]');
    if (d) deleteAmdKey(parseInt(d.dataset.amddel, 10), d);
  };
  tb.onchange = e => {
    const r = e.target.closest('input[data-amdr]');
    if (r) markRowDirty(r.closest('tr'), 'amd');
  };
}

function stageAllAmdKeysRouting(enabled) {
  document.querySelectorAll('#amdKeysBody input[data-amdr]').forEach(cb => {
    const snap = amdKeySnapshot[parseInt(cb.dataset.amdr, 10)];
    if (snap && snap.routing === enabled) return;
    cb.checked = enabled;
    markRowDirty(cb.closest('tr'), 'amd');
  });
}
// 清空代理（暂存）：全部 key 清绑定 + 路由全部禁用，与 OpenRouter 页同语义
function stageClearAmdKeyProxies() {
  document.querySelectorAll('#amdKeysBody tr').forEach(row => {
    const main = row.querySelector('.eg-input[data-eg="amd"][data-slot="main"]');
    if (!main) return;
    let dirty = stageRowBindingClear(row, 'amd');
    const cb = row.querySelector('input[data-amdr]');
    if (cb && cb.checked) { cb.checked = false; dirty = true; }
    if (dirty) markRowDirty(row, 'amd');
  });
}
async function saveAmdKeyEdits() {
  const rows = [...document.querySelectorAll('#amdKeysBody tr')].filter(r => r.querySelector('.eg-input'));
  let changed = 0, failed = 0;
  for (const row of rows) {
    const mainInput = row.querySelector('.eg-input[data-eg="amd"][data-slot="main"]');
    const idx = parseInt(mainInput.dataset.egid, 10);
    const main = mainInput.dataset.value || '';
    const backup = row.querySelector('.eg-input[data-eg="amd"][data-slot="backup"]').dataset.value || '';
    const rcb = row.querySelector('input[data-amdr]');
    const snap = amdKeySnapshot[idx] || { main: '', backup: '', routing: true };
    try {
      if (main !== snap.main || backup !== snap.backup) {
        await api('POST', '/amd/keys/proxy', { index: idx, main: main, backup: backup });
      }
      if (rcb && rcb.checked !== snap.routing) {
        await api('POST', '/amd/keys/routing', { index: idx, enabled: rcb.checked });
      }
      if (main !== snap.main || backup !== snap.backup || (rcb && rcb.checked !== snap.routing)) changed++;
      row.classList.remove('tr-dirty');
    } catch (e) { failed++; }
  }
  clearDirtyMarks('amdKeysBody', 'amdKeysSaveBtn');
  if (failed) toast(T('Save failed for ') + failed + T(' account(s)'), 'error');
  else if (!changed) toast(T('No changes to save'), 'info');
  else toast(T('Saved ') + changed + T(' account change(s)'), 'success');
  loadAmdConfig(); loadStats();
}

function resetAmdKeyEdits() {
  clearDirtyMarks('amdKeysBody', 'amdKeysSaveBtn');
  loadAmdConfig();
}

async function deleteAmdKey(index, btn) {
  if (!confirm(T('Delete this key?'))) return;
  const original = btn ? btn.innerHTML : '';
  if (btn) btn.disabled = true;
  try {
    await api('POST', '/amd/keys/delete', { index: index });
    toast(T('Key #') + (index + 1) + T(' (deleted)'), 'success');
  } catch (e) {
    toast(T('Delete failed: ') + e.message, 'error');
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = original; }
    loadAmdConfig(); loadStats();
  }
}

async function testAmdKey(index, btn) {
  const original = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span>Testing'; }
  try {
    const d = await api('POST', '/amd/keys/test', {
      index: index,
      model: (_('amdProbeModel') ? _('amdProbeModel').value : '')
    });
    const r = d.data || {};
    const label = { active: T('OK'), cooldown: T('Cooldown'), error: T('Error') }[r.status] || r.status;
    let msg = T('Key #') + (index + 1) + T(' (') + (r.keyMask || '') + T(') — ') + label;
    if (r.model) msg += T(' via ') + r.model;
    if (r.latencyMs != null) msg += ' (' + r.latencyMs + 'ms)';
    if (r.status === 'active') msg += T(' — cooldown cleared');
    if (r.cooldownUntil) msg += T('\nEstimated recovery: ') + fmtWhen(r.cooldownUntil) + (r.remaining ? T(' (remaining ') + r.remaining + T(')') : '');
    if (r.reason && r.reason !== 'ok') msg += '\n' + r.reason;
    const type = r.status === 'active' ? 'success' : (r.status === 'cooldown' ? 'warning' : 'error');
    toast(msg, type, 6000);
  } catch (e) {
    toast(T('Test failed: ') + e.message, 'error');
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = original; }
    loadAmdConfig();
  }
}

async function batchTestAmdKeys() {
  const btn = _('amdTestBtn');
  const idxs = Object.keys(amdKeySnapshot).map(Number).sort((a, b) => a - b);
  if (!idxs.length) { toast(T('No keys to test'), 'info'); return; }
  const original = btn ? btn.innerHTML : '';
  if (btn) btn.disabled = true;
  let ok = 0, cool = 0, err = 0;
  try {
    for (let i = 0; i < idxs.length; i++) {
      if (btn) btn.innerHTML = '<span class="loading"></span>' + (i + 1) + '/' + idxs.length;
      try {
        const d = await api('POST', '/amd/keys/test', {
          index: idxs[i],
          model: (_('amdProbeModel') ? _('amdProbeModel').value : '')
        });
        const r = d.data || {};
        if (r.status === 'active') ok++;
        else if (r.status === 'cooldown') cool++;
        else err++;
      } catch (e) { err++; }
    }
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = original; }
  }
  toast(T('Batch test done: ') + idxs.length + T(' tested, ') + ok + T(' ok') +
    (cool ? T(', ') + cool + T(' cooling') : '') + (err ? T(', ') + err + T(' error') : ''),
    err ? 'warning' : 'success', 6000);
  loadAmdConfig();
}

function openAmdKeyAddDialog() {
  _('amdKeyAddInput').value = '';
  _('amdKeyAddDialog').showModal();
}
function closeAmdKeyAddDialog() { _('amdKeyAddDialog').close(); }
async function amdKeyAddFromDialog() {
  const lines = _('amdKeyAddInput').value.split('\n').map(s => s.trim()).filter(Boolean);
  if (!lines.length) { toast(T('Enter at least one key'), 'error'); return; }
  const cur = (amdCfgCache.keys && amdCfgCache.keys.length) ? amdCfgCache.keys : [];
  const fresh = lines.filter(l => !cur.includes(l));
  if (!fresh.length) { toast(T('No new keys (all already in the pool)'), 'info'); return; }
  const btn = _('amdKeyAddSubmit');
  btn.disabled = true;
  try {
    await api('POST', '/amd/config/update', { keys: cur.concat(fresh) });
    toast(T('Added ') + fresh.length + T(' key(s)'), 'success');
    closeAmdKeyAddDialog();
    loadAmdConfig(); loadStats();
  } catch (e) {
    toast(T('Save failed: ') + e.message, 'error');
  } finally {
    btn.disabled = false;
  }
}

// ========== TokenHarbor page ==========
// TokenHarbor key 池（tkhb: 前缀平台）：协议与 AMDCloud/OpenRouter 页同构。
let thCfgCache = {};
let thKeySnapshot = {};

async function loadTHConfig() {
  try {
    await loadProxyListCache();
    const d = await api('GET', '/tokenharbor/config');
    const c = d.data;
    thCfgCache = c;
    const ks = c.keyStates || [];
    const sum = _('thSummary');
    if (sum) {
      const routed = ks.filter(k => k.routingEnabled !== false).length;
      const cooling = ks.filter(k => k.cooling).length;
      sum.textContent = ks.length + ' ' + T('keys') + ' · ' + routed + ' ' + T('active') + ' · ' + cooling + ' ' + T('cooling');
    }
    renderTHKeyStates(ks);
    _('thBaseURL').value = c.baseURL || '';
    _('thMaxConc').value = c.maxConcurrency || 8;
    _('thRetries').value = c.retries || 3;
    // 封禁过滤：黑名单语义是「不再对外提供」，页面模型列表同步剔除。
    await syncModelBlacklist();
    const models = (c.models || []).filter(m => m && !modelBlacklistIds.has(m.id));
    _('thModelsCount').textContent = '(' + models.length + T(' free models') + ')';
    const sel = _('thProbeModel');
    if (sel) {
      const prev = sel.value;
      sel.innerHTML = '<option value="">' + T('auto') + '</option>' +
        models.map(m => '<option value="' + esc(String(m.id || '').replace(/^tkhb:/, '')) + '">' + esc(m.id) + '</option>').join('');
      if (Array.from(sel.options).some(o => o.value === prev)) sel.value = prev;
    }
    const tb = _('thModelsBody');
    if (tb) {
      // 展示样式与网关设置页一致：等宽 ID + 免费标签 + 上下文标签（Name 进 title）
      const row = inner => '<div style="display:flex;align-items:center;gap:10px;padding:8px 12px;margin:5px 0;background:rgba(148,163,184,.06);border:1px solid var(--border);border-radius:10px">' + inner + '</div>';
      tb.innerHTML = models.length
        ? models.map(m => row(
            '<span style="font-family:\'JetBrains Mono\',monospace;font-size:13px;flex:1"' + (m.name ? ' title="' + esc(m.name) + '"' : '') + '>' + esc(m.id) + '</span>' +
            '<span class="model-tag free">' + T('no charge') + '</span>' +
            wbContextTagHTML(m.context, T('Context')) +
            '<button class="btn btn-sm btn-danger" data-bl-ban="1" data-platform="tokenharbor" data-model="' + esc(m.id) + '" data-refresh="th" style="white-space:nowrap">' + T('Ban') + '</button>')).join('')
        : '<div class="empty">' + T((thCfgCache.keyStates || []).some(k => k.routingEnabled !== false)
            ? 'No free models yet — click Refresh models'
            : 'Models appear here once a credential is enabled — the catalog syncs from upstream') + '</div>';
    }
  } catch (e) { /* ignore */ }
}

async function saveTHConfig() {
  // key 列表不随本表单提交（增删在 key 表的添加凭据/删除按钮里）
  const body = {
    baseURL: _('thBaseURL').value.trim(),
    maxConcurrency: parseInt(_('thMaxConc').value) || 8,
    retries: parseInt(_('thRetries').value) || 3
  };
  try {
    await api('POST', '/tokenharbor/config/update', body);
    toast(T('TokenHarbor config saved'), 'success');
    loadTHConfig();
  } catch (e) { toast(T('Save failed: ') + e.message, 'error'); }
}

async function refreshTHModels() {
  try {
    const d = await api('POST', '/tokenharbor/models/refresh');
    if (d && d.success === false) { toast(T(d.message || 'Refresh failed: '), 'error'); return; }
    await loadTHConfig();
    toast(T('Catalog refreshed') + ' (' + _('thModelsCount').textContent.replace(/[()]/g, '') + ')', 'success');
    loadTHConfig();
  } catch (e) { toast(T('Refresh failed: ') + e.message, 'error'); }
}

// key 状态表：key 掩码 / 今日累计 token / 最近使用 / 冷却 / 代理绑定 / 测试 /
// 删除 / 启用（暂存编辑）。
function renderTHKeyStates(ks) {
  const tb = _('thKeysBody');
  if (!tb) return;
  thKeySnapshot = {};
  tb.innerHTML = ks.length
    ? ks.map(k => {
        const cool = k.cooling
          ? '<span class="status cooldown"><span class="status-dot cooldown"></span>' + T('Cooldown') + '</span>' +
            (k.cooldownUntil ? '<div style="font-size:10px;color:var(--text3);margin-top:2px;white-space:nowrap" title="' + esc(fmtWhen(k.cooldownUntil)) + '">' + T('Recovers ') + esc(fmtShort(k.cooldownUntil)) + '</div>' : '')
          : '<span class="status active"><span class="status-dot active"></span>' + T('Active') + '</span>';
        const pMain = k.proxyMain || '';
        const pBackup = k.proxyBackup || '';
        const backupLocked = !backupProxyOn || pMain === '' || pMain === EGRESS_DIRECT;
        const backupVal = backupLocked ? pMain : pBackup;
        const isStale = proxyStale;
        const bind = '<td style="white-space:nowrap">' +
          egComboHTML('th', k.index, 'main', pMain, T('Main exit — used whenever available')) + ' ' +
          egComboHTML('th', k.index, 'backup', backupVal, T('Backup exit — used when main is cooling/removed'), backupLocked) +
          (isStale(pMain) || isStale(pBackup) ? ' <span title="' + esc(T('Bound proxy is no longer in the proxy pool — skipped until fixed')) + '" style="color:var(--danger)">⚠</span>' : '') +
          '</td>';
        const routed = k.routingEnabled !== false;
        thKeySnapshot[k.index] = { main: pMain, backup: backupVal, routing: routed };
        const routing = '<td style="text-align:center">' +
          '<input type="checkbox" data-thr="' + k.index + '"' + (routed ? ' checked' : '') + ' title="' + esc(T('Participates in request routing')) + '">' + '</td>';
        const actions = '<td style="white-space:nowrap;text-align:center">' +
          '<button class="btn btn-sm" data-tht="' + k.index + '">Test</button> ' +
          '<button class="btn btn-sm btn-danger" data-thdel="' + k.index + '" title="' + esc(T('Remove this key from the pool')) + '">Delete</button>' + '</td>';
        return '<tr draggable="true" data-drag-id="' + k.index + '"><td>#' + (k.index + 1) + '</td>' +
          '<td style="font-family:monospace;font-size:11px">' + esc(k.keyMask) + '</td>' +
          '<td>' + cool + '</td>' +
          keyTokensCell(k) +
          keyLastUsedCell(k) +
          bind +
          actions +
          routing + '</tr>';
      }).join('')
    : '<tr><td colspan="8" class="empty">No TokenHarbor keys configured</td></tr>';
  tb.onclick = e => {
    const t = e.target.closest('button[data-tht]');
    if (t) { testTHKey(parseInt(t.dataset.tht, 10), t); return; }
    const d = e.target.closest('button[data-thdel]');
    if (d) deleteTHKey(parseInt(d.dataset.thdel, 10), d);
  };
  tb.onchange = e => {
    const r = e.target.closest('input[data-thr]');
    if (r) markRowDirty(r.closest('tr'), 'th');
  };
}

function stageAllTHKeysRouting(enabled) {
  document.querySelectorAll('#thKeysBody input[data-thr]').forEach(cb => {
    const snap = thKeySnapshot[parseInt(cb.dataset.thr, 10)];
    if (snap && snap.routing === enabled) return;
    cb.checked = enabled;
    markRowDirty(cb.closest('tr'), 'th');
  });
}
// 清空代理（暂存）：全部 key 清绑定 + 路由全部禁用，与 AMDCloud/OpenRouter 页同语义
function stageClearTHKeyProxies() {
  document.querySelectorAll('#thKeysBody tr').forEach(row => {
    const main = row.querySelector('.eg-input[data-eg="th"][data-slot="main"]');
    if (!main) return;
    let dirty = stageRowBindingClear(row, 'th');
    const cb = row.querySelector('input[data-thr]');
    if (cb && cb.checked) { cb.checked = false; dirty = true; }
    if (dirty) markRowDirty(row, 'th');
  });
}
async function saveTHKeyEdits() {
  const rows = [...document.querySelectorAll('#thKeysBody tr')].filter(r => r.querySelector('.eg-input'));
  let changed = 0, failed = 0;
  for (const row of rows) {
    const mainInput = row.querySelector('.eg-input[data-eg="th"][data-slot="main"]');
    const idx = parseInt(mainInput.dataset.egid, 10);
    const main = mainInput.dataset.value || '';
    const backup = row.querySelector('.eg-input[data-eg="th"][data-slot="backup"]').dataset.value || '';
    const rcb = row.querySelector('input[data-thr]');
    const snap = thKeySnapshot[idx] || { main: '', backup: '', routing: true };
    try {
      if (main !== snap.main || backup !== snap.backup) {
        await api('POST', '/tokenharbor/keys/proxy', { index: idx, main: main, backup: backup });
      }
      if (rcb && rcb.checked !== snap.routing) {
        await api('POST', '/tokenharbor/keys/routing', { index: idx, enabled: rcb.checked });
      }
      if (main !== snap.main || backup !== snap.backup || (rcb && rcb.checked !== snap.routing)) changed++;
      row.classList.remove('tr-dirty');
    } catch (e) { failed++; }
  }
  clearDirtyMarks('thKeysBody', 'thKeysSaveBtn');
  if (failed) toast(T('Save failed for ') + failed + T(' account(s)'), 'error');
  else if (!changed) toast(T('No changes to save'), 'info');
  else toast(T('Saved ') + changed + T(' account change(s)'), 'success');
  loadTHConfig(); loadStats();
}

function resetTHKeyEdits() {
  clearDirtyMarks('thKeysBody', 'thKeysSaveBtn');
  loadTHConfig();
}

async function deleteTHKey(index, btn) {
  if (!confirm(T('Delete this key?'))) return;
  const original = btn ? btn.innerHTML : '';
  if (btn) btn.disabled = true;
  try {
    await api('POST', '/tokenharbor/keys/delete', { index: index });
    toast(T('Key #') + (index + 1) + T(' (deleted)'), 'success');
  } catch (e) {
    toast(T('Delete failed: ') + e.message, 'error');
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = original; }
    loadTHConfig(); loadStats();
  }
}

async function testTHKey(index, btn) {
  const original = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span>Testing'; }
  try {
    const d = await api('POST', '/tokenharbor/keys/test', {
      index: index,
      model: (_('thProbeModel') ? _('thProbeModel').value : '')
    });
    const r = d.data || {};
    const label = { active: T('OK'), cooldown: T('Cooldown'), error: T('Error') }[r.status] || r.status;
    let msg = T('Key #') + (index + 1) + T(' (') + (r.keyMask || '') + T(') — ') + label;
    if (r.model) msg += T(' via ') + r.model;
    if (r.latencyMs != null) msg += ' (' + r.latencyMs + 'ms)';
    if (r.status === 'active') msg += T(' — cooldown cleared');
    if (r.cooldownUntil) msg += T('\nEstimated recovery: ') + fmtWhen(r.cooldownUntil) + (r.remaining ? T(' (remaining ') + r.remaining + T(')') : '');
    if (r.reason && r.reason !== 'ok') msg += '\n' + r.reason;
    const type = r.status === 'active' ? 'success' : (r.status === 'cooldown' ? 'warning' : 'error');
    toast(msg, type, 6000);
  } catch (e) {
    toast(T('Test failed: ') + e.message, 'error');
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = original; }
    loadTHConfig();
  }
}

async function batchTestTHKeys() {
  const btn = _('thTestBtn');
  const idxs = Object.keys(thKeySnapshot).map(Number).sort((a, b) => a - b);
  if (!idxs.length) { toast(T('No keys to test'), 'info'); return; }
  const original = btn ? btn.innerHTML : '';
  if (btn) btn.disabled = true;
  let ok = 0, cool = 0, err = 0;
  try {
    for (let i = 0; i < idxs.length; i++) {
      if (btn) btn.innerHTML = '<span class="loading"></span>' + (i + 1) + '/' + idxs.length;
      try {
        const d = await api('POST', '/tokenharbor/keys/test', {
          index: idxs[i],
          model: (_('thProbeModel') ? _('thProbeModel').value : '')
        });
        const r = d.data || {};
        if (r.status === 'active') ok++;
        else if (r.status === 'cooldown') cool++;
        else err++;
      } catch (e) { err++; }
    }
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = original; }
  }
  toast(T('Batch test done: ') + idxs.length + T(' tested, ') + ok + T(' ok') +
    (cool ? T(', ') + cool + T(' cooling') : '') + (err ? T(', ') + err + T(' error') : ''),
    err ? 'warning' : 'success', 6000);
  loadTHConfig();
}

function openTHKeyAddDialog() {
  _('thKeyAddInput').value = '';
  _('thKeyAddDialog').showModal();
}
function closeTHKeyAddDialog() { _('thKeyAddDialog').close(); }
async function thKeyAddFromDialog() {
  const lines = _('thKeyAddInput').value.split('\n').map(s => s.trim()).filter(Boolean);
  if (!lines.length) { toast(T('Enter at least one key'), 'error'); return; }
  const cur = (thCfgCache.keys && thCfgCache.keys.length) ? thCfgCache.keys : [];
  const fresh = lines.filter(l => !cur.includes(l));
  if (!fresh.length) { toast(T('No new keys (all already in the pool)'), 'info'); return; }
  const btn = _('thKeyAddSubmit');
  btn.disabled = true;
  try {
    await api('POST', '/tokenharbor/config/update', { keys: cur.concat(fresh) });
    toast(T('Added ') + fresh.length + T(' key(s)'), 'success');
    closeTHKeyAddDialog();
    loadTHConfig(); loadStats();
  } catch (e) {
    toast(T('Save failed: ') + e.message, 'error');
  } finally {
    btn.disabled = false;
  }
}

/* ================= Other (multi sub-platform key pools) ================= */
let otherConfig = null;
let currentOtherSlug = '';
let otherSelectedModels = [];   // 已勾选模型 id（变更即时保存）
let otherKnownModels = [];      // Known = Fetched ∪ Manual
let otherKeySnapshot = {};      // 已保存的绑定/路由快照（id → {main, backup, routing}）
let otherSaveSelectedTimer = null;

function currentOtherPlatform() {
  if (!otherConfig || !currentOtherSlug) return null;
  return (otherConfig.platforms || []).find(p => p.slug === currentOtherSlug) || null;
}

async function loadOtherConfig() {
  let d;
  try { d = await api('GET', '/other/config'); }
  catch (e) { d = { data: { platforms: [], useProxies: null } }; }
  otherConfig = d.data || { platforms: [], useProxies: null };
  const ps = otherConfig.platforms || [];
  const sel = _('otherPlatformSelect');
  const prev = currentOtherSlug;
  // 初始态：value="" 是纯提示项，不对应任何子平台，因此不自动选中第一个。
  sel.innerHTML = ps.length
    ? '<option value="">' + T('Select a sub-platform to view details') + '</option>' +
      ps.map(p => '<option value="' + esc(p.slug) + '">' + esc(p.name || p.slug) + ' (' + esc(p.prefix) + ')</option>').join('')
    : '<option value="">' + T('No sub-platform yet — click "New sub-platform" to add one') + '</option>';
  if (ps.length && prev && ps.some(p => p.slug === prev)) currentOtherSlug = prev;
  else currentOtherSlug = '';
  sel.value = currentOtherSlug;
  const p = currentOtherPlatform();
  _('otherPlatformBody').style.display = p ? 'block' : 'none';
  if (!p) {
    _('otherSummary').textContent = ps.length
      ? T('Select a sub-platform on the left.')
      : T('No sub-platform yet. Click "New sub-platform" to add one.');
    return;
  }
  _('otherSummary').textContent = ps.length + T(' sub-platform(s). Models are exposed as <prefix>:<model-id>');
  _('otherName').value = p.name || '';
  _('otherPrefix').value = p.prefix || '';
  _('otherBaseURL').value = p.baseURL || '';
  _('otherMaxConc').value = p.maxConcurrency || 8;
  _('otherRetries').value = p.retries || 3;
  otherKnownModels = p.models || [];
  otherSelectedModels = p.selectedModels || [];
  if (_('otherLivePrompt') && !_('otherLivePrompt').value) _('otherLivePrompt').value = T('Test message, please reply with hello only');
  renderOtherModelSelect(otherKnownModels, otherSelectedModels, p.manualModels || []);

  renderOtherKeys(p.keyStates || []);
}

_('otherPlatformSelect').addEventListener('change', function () { currentOtherSlug = this.value; loadOtherConfig(); });

/* ---- 多选下拉：可用模型 ---- */
function renderOtherModelSelect(models, selected, manual) {
  const list = _('otherModelSelectList');
  const manualIds = manual || [];
  if (!models || !models.length) {
    list.innerHTML = '<div class="ms-empty">' + T('No models yet — add manually or add a key and click Refresh models') + '</div>';
  } else {
    list.innerHTML = models.map(m => {
      // 只有手动添加的模型可删；上游同步来的删了会被下次刷新再拉回来。
      const del = manualIds.indexOf(m) >= 0
        ? '<button class="ms-del" title="' + esc(T('Remove this manually added model')) +
          '" onclick="removeOtherManualModel(\'' + esc(m) + '\', event)">\u00d7</button>'
        : '';
      return '<label class="ms-item" data-id="' + esc(m) + '">' +
        '<input type="checkbox" value="' + esc(m) + '"' + (selected.indexOf(m) >= 0 ? ' checked' : '') +
        ' onchange="onOtherModelToggle(this)"> <span title="' + esc(m) + '">' + esc(m) + '</span>' + del + '</label>';
    }).join('');
  }
  _('otherModelSelectToggle').textContent = otherModelSelectSummary();
  _('otherModelSelectSearch').value = '';
}

function otherModelSelectSummary() {
  const n = otherSelectedModels.length;
  // 无模型时给出下一步指引，而非一个无解释的破折号。
  if (!otherKnownModels.length) return T('Add a credential, then refresh models or add one manually');
  return n ? n + ' / ' + otherKnownModels.length + T(' enabled') : T('none enabled');
}

// otherProbeModelValue 探测模型：探测显示已从面板移除，但 Test 逻辑不变——
// 未显式指定时一律取第一个已勾选模型（后端 ProbeModel 同口径）。
function otherProbeModelValue() {
  const p = currentOtherPlatform();
  if (p && p.probeModel) return p.probeModel;
  return otherSelectedModels.length ? otherSelectedModels[0] : '';
}

function toggleOtherModelSelect() {
  const panel = _('otherModelSelectPanel');
  const wrap = _('otherModelSelect');
  panel.style.display = panel.style.display === 'none' ? 'block' : 'none';
  if (panel.style.display !== 'block') { wrap.classList.remove('drop-up'); return; }
  _('otherModelSelectSearch').focus();
  placeOtherModelSelect();
}

// placeOtherModelSelect 下方剩余高度装不下浮层时改为向上展开，
// 否则靠视口底部的列表会被裁掉（面板高度随勾选项数量变化，故每次展开重算）。
function placeOtherModelSelect() {
  const panel = _('otherModelSelectPanel');
  const wrap = _('otherModelSelect');
  wrap.classList.remove('drop-up');
  const below = window.innerHeight - wrap.getBoundingClientRect().bottom;
  if (panel.offsetHeight + 8 > below) wrap.classList.add('drop-up');
}
window.addEventListener('resize', function () {
  if (_('otherModelSelectPanel').style.display === 'block') placeOtherModelSelect();
});

function filterOtherModelSelect() {
  const q = _('otherModelSelectSearch').value.trim().toLowerCase();
  Array.prototype.forEach.call(_('otherModelSelectList').querySelectorAll('.ms-item'), function (el) {
    el.style.display = !q || el.dataset.id.toLowerCase().indexOf(q) >= 0 ? '' : 'none';
  });
}

function onOtherModelToggle(cb) {
  const id = cb.value;
  const i = otherSelectedModels.indexOf(id);
  if (cb.checked && i < 0) otherSelectedModels.push(id);
  if (!cb.checked && i >= 0) otherSelectedModels.splice(i, 1);
  _('otherModelSelectToggle').textContent = otherModelSelectSummary();

  queueSaveOtherSelected();
}

function queueSaveOtherSelected() {
  if (otherSaveSelectedTimer) clearTimeout(otherSaveSelectedTimer);
  otherSaveSelectedTimer = setTimeout(saveOtherSelected, 300);
}

async function saveOtherSelected() {
  const p = currentOtherPlatform();
  if (!p) return;
  try {
    const r = await api('POST', '/other/models/selected', { slug: p.slug, selected: otherSelectedModels });
    otherSelectedModels = (r.data && r.data.platform && r.data.platform.selectedModels) || otherSelectedModels;
    toast(T('Models updated'), 'success');
  } catch (e) { toast(T('Save failed: ') + e.message, 'error'); }
}

// otherLiveRequest 用第一个已启用凭据发起一次真实会话，并把上游回复回显。
// 三类前置缺失分别提示：无可用模型 / 无已启用凭据 / 成功失败内容。
async function otherLiveRequest() {
  const p = currentOtherPlatform();
  if (!p) return;
  const prompt = (_('otherLivePrompt') ? _('otherLivePrompt').value : '').trim();
  if (!otherSelectedModels.length) { toast(T('No available models'), 'error'); return; }
  const enabled = (p.keyStates || []).filter(k => k.routingEnabled);
  if (!enabled.length) { toast(T('No available credentials'), 'error'); return; }
  const btn = _('otherLiveBtn');
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span>' + T('Sending…'); }
  try {
    const r = await api('POST', '/other/chat', { slug: p.slug, model: otherProbeModelValue(), prompt: prompt });
    const d = r.data || {};
    toast(T('Request succeeded') + ': ' + (d.reply || '(empty reply)') + '  [' + d.model + ' · ' + d.keyMask + ' · ' + d.latencyMs + 'ms]', 'success', 12000);
  } catch (e) {
    const d = (e && e.data) || {};
    toast(T('Request failed') + ': ' + (d.reason || e.message), 'error', 12000);
  } finally {
    if (btn) { btn.disabled = false; btn.textContent = T('Real request'); }
  }
}

async function refreshOtherModels() {
  const p = currentOtherPlatform();
  if (!p) return;
  // 无凭据无法拉上游目录，先提示再中止。判定用 keyStates —— 平台对象只带这个
  // 字段，没有 p.keys；启用口径 routingEnabled !== false，与后端 RoutingEnabled
  // 缺省 true 一致：全部禁用时同步无意义，后端同样会拒。
  const ks = p.keyStates || [];
  if (!ks.length) { toast(T('Add a credential first'), 'error'); return; }
  if (!ks.some(k => k.routingEnabled !== false)) { toast(T('No available credentials'), 'error'); return; }
  toast(T('Syncing models…'));
  try {
    const r = await api('POST', '/other/models/refresh', { slug: p.slug });
    const np = r.data && r.data.platform;
    if (np) {
      otherKnownModels = np.models || [];
      otherSelectedModels = np.selectedModels || [];
      renderOtherModelSelect(otherKnownModels, otherSelectedModels, np.manualModels || []);

      renderOtherKeys(np.keyStates || []);
    }
    toast(T('Model catalog synced'), 'success');
  } catch (e) { toast(T('Refresh failed: ') + e.message, 'error'); }
}

// removeOtherManualModel 删掉一个手动添加的模型（后端同时取消其勾选）。
async function removeOtherManualModel(id, ev) {
  if (ev) { ev.preventDefault(); ev.stopPropagation(); }   // 否则会连带触发 label 勾选
  const p = currentOtherPlatform();
  if (!p || !id) return;
  // 手动模型删了可原样再加回来，不做二次确认；结果由 toast 反馈。
  try {
    const r = await api('POST', '/other/models/manual/delete', { slug: p.slug, model: id });
    const np = r.data && r.data.platform;
    if (np) {
      otherKnownModels = np.models || [];
      otherSelectedModels = np.selectedModels || [];
      renderOtherModelSelect(otherKnownModels, otherSelectedModels, np.manualModels || []);
    }
    toast(T('Manual model removed'), 'success');
  } catch (e) { toast(T('Remove failed: ') + e.message, 'error'); }
}

async function addOtherManualModel() {
  const p = currentOtherPlatform();
  const input = _('otherManualModel');
  const m = input.value.trim();
  if (!p || !m) { toast(T('Enter a model id'), 'error'); return; }
  try {
    const r = await api('POST', '/other/models/manual', { slug: p.slug, model: m });
    const np = r.data && r.data.platform;
    if (np) {
      otherKnownModels = np.models || [];
      otherSelectedModels = np.selectedModels || [];
      renderOtherModelSelect(otherKnownModels, otherSelectedModels, np.manualModels || []);

    }
    input.value = '';
    toast(T('Added') + ': ' + m, 'success');
  } catch (e) { toast(T('Add failed: ') + e.message, 'error'); }
}


/* ---- 账号（key）表（与 AMDCloud 同构：DOM diff + 事件委托 + 拖拽） ---- */
function renderOtherKeys(ks) {
  const tb = _('otherKeysBody');
  if (!tb) return;
  otherKeySnapshot = {};
  if (!ks.length) { tb.innerHTML = '<tr><td colspan="8" class="empty">' + T('No keys configured') + '</td></tr>'; return; }
  tb.innerHTML = ks.map((k, i) => {
    const cool = k.cooling
      ? '<span class="status cooldown"><span class="status-dot cooldown"></span>' + T('Cooldown') + '</span>' +
        (k.cooldownUntil ? '<div style="font-size:10px;color:var(--text3);margin-top:2px;white-space:nowrap" title="' + esc(fmtWhen(k.cooldownUntil)) + '">' + T('Recovers ') + esc(fmtShort(k.cooldownUntil)) + '</div>' : '')
      : '<span class="status active"><span class="status-dot active"></span>' + T('Active') + '</span>';
    const pMain = k.proxyMain || '';
    const pBackup = k.proxyBackup || '';
    const backupLocked = !backupProxyOn || pMain === '' || pMain === EGRESS_DIRECT;
    const backupVal = backupLocked ? pMain : pBackup;
    const stale = proxyStale;
    const bind = '<td style="white-space:nowrap">' +
      egComboHTML('other', k.index, 'main', pMain, T('Main exit — used whenever available')) + ' ' +
      egComboHTML('other', k.index, 'backup', backupVal, T('Backup exit — used when main is cooling/removed'), backupLocked) +
      (stale(pMain) || stale(pBackup) ? ' <span title="' + esc(T('Bound proxy is no longer in the proxy pool — skipped until fixed')) + '" style="color:var(--danger)">⚠</span>' : '') +
      '</td>';
    const routed = k.routingEnabled !== false;
    otherKeySnapshot[k.index] = { main: pMain, backup: backupVal, routing: routed };
    const routing = '<td style="text-align:center">' +
      '<input type="checkbox" data-otr="' + k.index + '"' + (routed ? ' checked' : '') + ' title="' + esc(T('Participates in request routing')) + '">' + '</td>';
    const actions = '<td style="white-space:nowrap;text-align:center">' +
      '<button class="btn btn-sm" data-ott="' + k.index + '">' + T('Test') + '</button> ' +
      '<button class="btn btn-sm btn-danger" data-otdel="' + k.index + '" title="' + esc(T('Delete this key')) + '">' + T('Delete') + '</button>' + '</td>';
    return '<tr draggable="true" data-drag-id="' + k.index + '"><td>' + (i + 1) + '</td>' +
      '<td style="font-family:monospace;font-size:11px">' + esc(k.keyMask) + '</td>' +
      '<td>' + cool + '</td>' +
      keyTokensCell(k) +
      keyLastUsedCell(k) +
      bind + actions + routing + '</tr>';
  }).join('');
  tb.onclick = e => {
    const t = e.target.closest('button[data-ott]');
    if (t) { testOtherKey(t.dataset.ott, t); return; }
    const d = e.target.closest('button[data-otdel]');
    if (d) deleteOtherKey(d.dataset.otdel, d);
  };
  tb.onchange = e => {
    const r = e.target.closest('input[data-otr]');
    if (r) markRowDirty(r.closest('tr'), 'other');
  };
}

function stageAllOtherKeysRouting(enabled) {
  document.querySelectorAll('#otherKeysBody input[data-otr]').forEach(cb => {
    const snap = otherKeySnapshot[cb.dataset.otr];
    if (snap && snap.routing === enabled) return;
    cb.checked = enabled;
    markRowDirty(cb.closest('tr'), 'other');
  });
}
// 清空代理（暂存）：全部 key 清绑定 + 路由全部禁用，与 AMDCloud 页同语义
function stageClearOtherKeyProxies() {
  document.querySelectorAll('#otherKeysBody tr').forEach(row => {
    const main = row.querySelector('.eg-input[data-eg="other"][data-slot="main"]');
    if (!main) return;
    let dirty = stageRowBindingClear(row, 'other');
    const cb = row.querySelector('input[data-otr]');
    if (cb && cb.checked) { cb.checked = false; dirty = true; }
    if (dirty) markRowDirty(row, 'other');
  });
}

async function saveOtherKeyEdits() {
  const p = currentOtherPlatform();
  if (!p) return;
  const rows = [...document.querySelectorAll('#otherKeysBody tr')].filter(r => r.querySelector('.eg-input'));
  let changed = 0, failed = 0;
  for (const row of rows) {
    const mainInput = row.querySelector('.eg-input[data-eg="other"][data-slot="main"]');
    const index = Number(mainInput.dataset.egid);
    const main = mainInput.dataset.value || '';
    const backup = row.querySelector('.eg-input[data-eg="other"][data-slot="backup"]').dataset.value || '';
    const rcb = row.querySelector('input[data-otr]');
    const snap = otherKeySnapshot[index] || { main: '', backup: '', routing: true };
    try {
      if (main !== snap.main || backup !== snap.backup) {
        await api('POST', '/other/keys/proxy', { slug: p.slug, index: index, main: main, backup: backup });
      }
      if (rcb && rcb.checked !== snap.routing) {
        await api('POST', '/other/keys/routing', { slug: p.slug, index: Number(rcb.dataset.otr), enabled: rcb.checked });
      }
      if (main !== snap.main || backup !== snap.backup || (rcb && rcb.checked !== snap.routing)) changed++;
      row.classList.remove('tr-dirty');
    } catch (e) { failed++; }
  }
  clearDirtyMarks('otherKeysBody', 'otherKeysSaveBtn');
  if (failed) toast(T('Save failed for ') + failed + T(' account(s)'), 'error');
  else if (!changed) toast(T('No changes to save'), 'info');
  else toast(T('Saved ') + changed + T(' account change(s)'), 'success');
  loadOtherConfig(); loadStats();
}

function resetOtherKeyEdits() {
  clearDirtyMarks('otherKeysBody', 'otherKeysSaveBtn');
  loadOtherConfig();
}

async function deleteOtherKey(id, btn) {
  if (!confirm(T('Delete this key?'))) return;
  const original = btn ? btn.innerHTML : '';
  if (btn) btn.disabled = true;
  try {
    const p = currentOtherPlatform();
    await api('POST', '/other/keys/delete', { slug: p.slug, index: Number(id) });
    toast(T('Deleted'), 'success');
  } catch (e) {
    toast(T('Delete failed: ') + e.message, 'error');
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = original; }
    loadOtherConfig(); loadStats();
  }
}

/* ---- 测试（与 AMDCloud 同构：按钮内 spinner + 结果 toast） ---- */
async function testOtherKey(id, btn) {
  const original = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span>Testing'; }
  try {
    const p = currentOtherPlatform();
    const d = await api('POST', '/other/keys/test', {
      slug: p.slug,
      index: Number(id),
      model: otherProbeModelValue()
    });
    const r = (d.data && d.data) || {};
    const ok = d.success === true;
    const msg = ok
      ? T('Test passed') + (r.latencyMs != null ? ' (' + r.latencyMs + 'ms)' : '') + (r.status === 'active' ? T(' — cooldown cleared') : '')
      : T('Test failed: ') + (r.reason || r.error || '');
    toast(msg, ok ? 'success' : 'error', 6000);
  } catch (e) {
    toast(T('Test failed: ') + e.message, 'error');
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = original; }
    loadOtherConfig();
  }
}

async function batchTestOtherKeys() {
  const p = currentOtherPlatform();
  if (!p) return;
  const btn = _('otherTestBtn');
  const ids = Object.keys(otherKeySnapshot);
  if (!ids.length) { toast(T('No keys to test'), 'info'); return; }
  const original = btn ? btn.innerHTML : '';
  if (btn) btn.disabled = true;
  let ok = 0, err = 0;
  try {
    for (let i = 0; i < ids.length; i++) {
      if (btn) btn.innerHTML = '<span class="loading"></span>' + (i + 1) + '/' + ids.length;
      try {
        const d = await api('POST', '/other/keys/test', {
          slug: p.slug,
          index: Number(ids[i]),
          model: otherProbeModelValue()
        });
        if (d.success === true) ok++; else err++;
      } catch (e) { err++; }
    }
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = original; }
  }
  toast(T('Batch test done: ') + ids.length + T(' tested, ') + ok + T(' ok') + (err ? T(', ') + err + T(' error') : ''),
    err ? 'warning' : 'success', 6000);
  loadOtherConfig();
}

/* ---- 子平台 CRUD / 配置保存 ---- */
function openOtherPlatformDialog() {
  _('opName').value = '';
  _('opPrefix').value = '';
  _('opBaseURL').value = '';
  _('otherPlatformDialog').showModal();
}
function closeOtherPlatformDialog() { _('otherPlatformDialog').close(); }

async function doOtherPlatformCreate() {
  const name = _('opName').value.trim();
  const prefix = _('opPrefix').value.trim().replace(/^:+/, '');
  const baseURL = _('opBaseURL').value.trim().replace(/\/+$/, '');
  if (!name || !prefix) { toast(T('Name and prefix are required'), 'error'); return; }
  try {
    const r = await api('POST', '/other/platforms', { name: name, prefix: prefix, baseURL: baseURL });
    closeOtherPlatformDialog();
    currentOtherSlug = (r.data && r.data.platform && r.data.platform.slug) || '';
    toast(T('Sub-platform created'), 'success');
    loadOtherConfig();
  } catch (e) { toast(T('Create failed: ') + e.message, 'error'); }
}


/* ---- 子平台管理弹窗：列表 / 删除 / 拖动排序 ---- */
var otherManageDragSlug = '';

function openOtherManageDialog() {
  renderOtherManageList();
  _('otherManageDialog').showModal();
}
function closeOtherManageDialog() { _('otherManageDialog').close(); }

function renderOtherManageList() {
  const box = _('otherManageList');
  if (!box) return;
  const ps = (otherConfig && otherConfig.platforms) || [];
  if (!ps.length) { box.innerHTML = '<div class="mg-empty">' + esc(T('No sub-platform yet. Click "New sub-platform" to add one.')) + '</div>'; return; }
  box.innerHTML = ps.map(p =>
    '<div class="mg-row" draggable="true" data-slug="' + esc(p.slug) + '">' +
      '<span class="mg-handle" title="' + esc(T('Drag to reorder')) + '">⣿</span>' +
      '<span class="mg-name">' + esc(p.name || p.slug) + '</span>' +
      '<span class="mg-prefix">' + esc(p.prefix) + '</span>' +
      '<span class="mg-meta" title="' + esc(p.slug + (p.baseURL ? ' · ' + p.baseURL : '')) + '">' + esc(p.slug) + ' · ' + esc(p.baseURL || '') + '</span>' +
      '<button class="btn btn-sm btn-danger" data-del="' + esc(p.slug) + '">' + esc(T('Delete')) + '</button>' +
    '</div>').join('');
  // 删除走事件委托：slug 只出现在 HTML 属性里，不拼进 JS 字符串字面量。
  box.querySelectorAll('.mg-row [data-del]').forEach(btn => {
    btn.addEventListener('click', ev => { ev.stopPropagation(); deleteOtherPlatformBySlug(btn.dataset.del); });
  });
  bindOtherManageDrag();
}

function bindOtherManageDrag() {
  const rows = [...document.querySelectorAll('#otherManageList .mg-row')];
  rows.forEach(row => {
    row.addEventListener('dragstart', ev => {
      otherManageDragSlug = row.dataset.slug;
      row.classList.add('dragging');
      ev.dataTransfer.effectAllowed = 'move';
      try { ev.dataTransfer.setData('text/plain', row.dataset.slug); } catch (e) {}
    });
    row.addEventListener('dragend', () => {
      row.classList.remove('dragging');
      rows.forEach(r => r.classList.remove('dragover'));
      otherManageDragSlug = '';
    });
    row.addEventListener('dragover', ev => {
      ev.preventDefault();
      ev.dataTransfer.dropEffect = 'move';
      if (row.dataset.slug === otherManageDragSlug) return;
      row.classList.add('dragover');
    });
    row.addEventListener('dragleave', () => row.classList.remove('dragover'));
    row.addEventListener('drop', ev => {
      ev.preventDefault();
      row.classList.remove('dragover');
      const src = otherManageDragSlug || (ev.dataTransfer ? ev.dataTransfer.getData('text/plain') : '');
      if (!src || src === row.dataset.slug) return;
      const list = _('otherManageList');
      const from = list.querySelector('.mg-row[data-slug="' + (window.CSS && CSS.escape ? CSS.escape(src) : src) + '"]');
      if (!from) return;
      // 以落点行为准：拖到上方则插到它前面，拖到下方则插到它后面。
      const r = row.getBoundingClientRect();
      const after = ev.clientY > r.top + r.height / 2;
      if (after) row.after(from); else row.before(from);
      otherManageDragSlug = '';
    });
  });
}

async function deleteOtherPlatformBySlug(slug) {
  const ps = (otherConfig && otherConfig.platforms) || [];
  const p = ps.find(x => x.slug === slug);
  if (!p) return;
  if (!confirm(T('Delete') + ' "' + (p.name || p.slug) + '" (' + p.prefix + ':)? ' + T('All its keys will be removed. This cannot be undone.'))) return;
  try {
    await api('POST', '/other/platforms/delete', { slug: slug });
    if (currentOtherSlug === slug) currentOtherSlug = '';
    toast(T('Deleted'), 'success');
    await loadOtherConfig(); loadStats();
    renderOtherManageList();
  } catch (e) { toast(T('Delete failed: ') + e.message, 'error'); }
}

async function saveOtherPlatformOrder() {
  const slugs = [...document.querySelectorAll('#otherManageList .mg-row')].map(r => r.dataset.slug);
  if (!slugs.length) { closeOtherManageDialog(); return; }
  try {
    await api('POST', '/other/platforms/reorder', { slugs: slugs });
    toast(T('Order saved'), 'success');
    closeOtherManageDialog();
    await loadOtherConfig();
  } catch (e) { toast(T('Save failed: ') + e.message, 'error'); }
}

async function saveOtherPlatformConfig() {
  const p = currentOtherPlatform();
  if (!p) return;
  const name = _('otherName').value.trim();
  const prefix = _('otherPrefix').value.trim().replace(/^:+/, '');
  const baseURL = _('otherBaseURL').value.trim().replace(/\/+$/, '');
  if (!name || !prefix) { toast(T('Name and prefix are required'), 'error'); return; }
  try {
    await api('POST', '/other/config/update', { slug: p.slug, name: name, prefix: prefix, baseURL: baseURL });
    toast(T('Saved'), 'success');
    loadOtherConfig();
  } catch (e) { toast(T('Save failed: ') + e.message, 'error'); }
}

async function saveOtherRateLimit() {
  const p = currentOtherPlatform();
  if (!p) return;
  try {
    await api('POST', '/other/config/update', {
      slug: p.slug,
      maxConcurrency: parseInt(_('otherMaxConc').value, 10) || 8,
      retries: parseInt(_('otherRetries').value, 10) || 3,
    });
    toast(T('Saved'), 'success');
    loadOtherConfig();
  } catch (e) { toast(T('Save failed: ') + e.message, 'error'); }
}

/* ---- 添加 key 弹窗 ---- */
function openOtherKeyAddDialog() {
  _('otherKeyAddInput').value = '';
  _('otherKeyAddDialog').showModal();
}
function closeOtherKeyAddDialog() { _('otherKeyAddDialog').close(); }

async function otherKeyAddFromDialog() {
  const p = currentOtherPlatform();
  const text = _('otherKeyAddInput').value.trim();
  if (!p || !text) { toast(T('Enter at least one key'), 'error'); return; }
  const keys = text.split('\n').map(s => s.trim()).filter(Boolean);
  try {
    const r = await api('POST', '/other/keys/update', { slug: p.slug, add: keys });
    closeOtherKeyAddDialog();
    const added = (r.data && r.data.added) != null ? r.data.added : keys.length;
    toast(T('Added') + ' ' + added + ' ' + T('keys'), 'success');
    loadOtherConfig();
  } catch (e) { toast(T('Add failed: ') + e.message, 'error'); }
}

/* 点击面板外部收起多选下拉 */
document.addEventListener('click', function (e) {
  const ms = document.getElementById('otherModelSelect');
  if (ms && !ms.contains(e.target)) {
    const panel = _('otherModelSelectPanel');
    if (panel) panel.style.display = 'none';
  }
});

async function loadOcSessions() {
  try {
    const d = await api('GET', '/opencode/sessions');
    renderOcSessions(d.data);
    return d.data;
  } catch (e) { return null; }
}

function renderOcSessions(s) {
  const rows = s.sessions || [];
  _('ocSessSummary').innerHTML = rows.length
    ? '<span style="color:' + (s.liveCount === s.total ? 'var(--accent2)' : 'var(--danger)') + '">' +
      s.liveCount + '/' + s.total + ' live</span>'
    : '';
  _('ocSessBody').innerHTML = rows.length
    ? rows.map(k => {
        const state = k.noKey
          ? '<span style="color:var(--text2)">no key</span>'
          : (k.live
              ? '<span style="color:var(--accent2)">live</span>'
              : '<span style="color:var(--danger)">not minted</span>');
        return '<tr><td>#' + (k.index + 1) + '</td>' +
          '<td style="font-family:monospace;font-size:11px">' + (k.session ? esc(k.session) + '…' : '-') + '</td>' +
          '<td>' + state + '</td>' +
          '<td style="font-size:12px">' + (k.createdAt ? esc(fmtWhen(k.createdAt)) : '-') + '</td></tr>';
      }).join('')
    : '<tr><td colspan="4" class="empty">No opencode keys configured</td></tr>';
}

// ========== Combos (alias models) ==========
const comboModels = { cline: [], trae: [], zen: [], workbuddy: [], amd: [], openrouter: [], tokenharbor: [] };

async function fillComboModels() {
  const platform = _('comboPlatform').value;
  const sel = _('comboTarget');
  try {
    if (!comboModels[platform].length) {
      if (platform === 'cline') {
        const d = await api('GET', '/models');
        // /v1/models 输出即 cline-free/ 前缀形态（apiModelList 已规范化），
        // 原样采用，面板所见即所调。
        comboModels.cline = (d.data.models || []).map(m => m.id).filter(Boolean);
      } else if (platform === 'trae') {
        const d = await api('GET', '/trae/config');
        comboModels.trae = (d.data.models || []).map(m => String(m.id || '').replace(/^trae:/, '')).filter(Boolean);
      } else if (platform === 'zen') {
        const d = await api('GET', '/opencode/models');
        comboModels.zen = (d.data.models || []).map(m => m.id).filter(Boolean);
      } else if (platform === 'workbuddy') {
        const all = await fetchWorkbuddyModels();
        comboModels.workbuddy = all.filter(wbModelMatchesFilter)
          .sort((a, b) => wbRate(a) - wbRate(b))
          .map(m => m.id).filter(Boolean);
      } else if (platform === 'openrouter') {
        const d = await api('GET', '/openrouter/config');
        comboModels.openrouter = (d.data.models || []).map(m => String(m.id || '').replace(/^oprt:/, '')).filter(Boolean);
      } else if (platform === 'amd') {
        const d = await api('GET', '/amd/config');
        comboModels.amd = (d.data.models || []).map(m => String(m.id || '').replace(/^amd:/, '')).filter(Boolean);
      } else if (platform === 'tokenharbor') {
        const d = await api('GET', '/tokenharbor/config');
        comboModels.tokenharbor = (d.data.models || []).map(m => String(m.id || '').replace(/^tkhb:/, '')).filter(Boolean);
      }
    }
    if (!comboModels[platform].length) { sel.innerHTML = '<option value="">No models on this platform</option>'; return; }
    sel.innerHTML = comboModels[platform].map(id => '<option value="' + esc(id) + '">' + esc(id) + '</option>').join('');
  } catch (e) { sel.innerHTML = '<option value="">Failed to load model list</option>'; }
}

async function loadCombos() {
  try {
    const d = await api('GET', '/combos');
    const combos = d.data.combos || [];
    if (!combos.length) { _('combosList').innerHTML = '<div class="hint">No combos yet — create one with the form above.</div>'; return; }
    _('combosList').innerHTML = '<div class="table-wrap"><table><thead><tr><th style="text-align:left">Alias ID</th><th>Platform</th><th style="text-align:left">Target model</th><th>Proxy</th><th>Created</th><th>Actions</th></tr></thead><tbody>' +
      combos.map(c => '<tr>' +
        '<td style="text-align:left;font-family:monospace;font-weight:600">' + esc(c.id) + '</td>' +
        '<td><span class="model-tag">' + esc(c.platform === 'zen' ? 'opencode' : c.platform) + '</span></td>' +
        '<td style="text-align:left;font-family:monospace">' + esc(c.target) + '</td>' +
        '<td>' + (c.useProxies ? '<span class="model-tag" style="color:var(--accent2)">socks5 pool</span>' : '-') + '</td>' +
        '<td style="font-size:11px">' + (c.createdAt ? new Date(c.createdAt).toLocaleString('en-US') : '-') + '</td>' +
        '<td><button class="btn btn-sm btn-danger" data-delcombo="' + esc(c.id) + '">Delete</button></td>' +
      '</tr>').join('') +
      '</tbody></table></div>';
    _('combosList').onclick = e => {
      const b = e.target.closest('button[data-delcombo]');
      if (b) deleteCombo(b.dataset.delcombo);
    };
  } catch (e) { _('combosList').textContent = T('Failed to load: ') + e.message; }
}

async function createCombo() {
  const id = _('comboId').value.trim();
  const platform = _('comboPlatform').value;
  const target = _('comboTarget').value;
  if (!id) { toast('Enter an alias ID', 'error'); return; }
  if (!target) { toast('Select a target model', 'error'); return; }
  try {
    await api('POST', '/combos/create', { id, platform, target, useProxies: _('comboUseProxies').checked });
    toast(T('Combo created: ') + id + ' → ' + target, 'success');
    _('comboId').value = '';
    _('comboUseProxies').checked = false;
    loadCombos();
  } catch (e) { toast(T('Create failed: ') + e.message, 'error'); }
}

async function deleteCombo(id) {
  if (!confirm(T('Delete combo ') + id + T('? Clients using this alias will no longer be able to call it.'))) return;
  try {
    await api('POST', '/combos/delete', { id });
    toast('Combo deleted', 'success');
    loadCombos();
  } catch (e) { toast(T('Delete failed: ') + e.message, 'error'); }
}

// ========== Init ==========
loadStats();
loadAccounts();
loadKeys();
loadModels();
loadConfig();
restoreTab();
// 拖拽排序（Account pool 三菜单）：submit 即时落库，reload 重拉对齐派生状态
// （ocKeysBody 的 index 暂存协议、Cline 的轮转游标、wb 的 order 持久化）。
enableRowDragReorder('accountTableBody', ids => api('POST', '/accounts/reorder', { order: ids }), loadAccounts);
enableRowDragReorder('ocKeysBody', ids => api('POST', '/opencode/keys/reorder', { order: ids.map(Number) }), loadOcConfig);
enableRowDragReorder('orKeysBody', ids => api('POST', '/openrouter/keys/reorder', { order: ids.map(Number) }), loadOrConfig);
enableRowDragReorder('amdKeysBody', ids => api('POST', '/amd/keys/reorder', { order: ids.map(Number) }), loadAmdConfig);
enableRowDragReorder('thKeysBody', ids => api('POST', '/tokenharbor/keys/reorder', { order: ids.map(Number) }), loadTHConfig);
enableRowDragReorder('otherKeysBody', ids => {
  const p = currentOtherSlug;
  return api('POST', '/other/keys/reorder', { slug: p, order: ids.map(Number) });
}, loadOtherConfig);
enableRowDragReorder('wbAccountsBody', ids => api('POST', '/workbuddy/reorder', { uids: ids }), loadWorkbuddyAccounts);
// 平台概览只在仪表盘可见时轮询（loadStats 现在打三个接口，隐藏页轮询纯浪费）
setInterval(() => { if (_('tab-dashboard').style.display !== 'none') loadStats(); }, 10000);
// live 会话状态：只在 opencode 页可见时轮询（与日志页同样的省流约定）。
setInterval(() => {
  if (_('tab-opencode').style.display !== 'none') loadOcSessions();
}, 20000);
setInterval(() => { if (logsAuto && _('tab-logs').style.display !== 'none') loadLogs(); }, 8000);
</script>
</body>
</html>`
