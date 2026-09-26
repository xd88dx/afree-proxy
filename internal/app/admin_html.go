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
  --accent:#8ab4f8;--accent2:#7ee2a8;--amber:#d9b04a;--danger:#e0705f;
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
  --accent:#33629c;--accent2:#2b7a4b;--amber:#96700f;--danger:#b3442f;
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

/* ===== Cards ===== */
.cards{display:grid;grid-template-columns:repeat(auto-fit,minmax(170px,1fr));gap:14px;margin-bottom:26px}
.card{background:var(--panel);border:1px solid var(--border);border-radius:var(--radius);padding:18px;position:relative;overflow:hidden;transition:.2s}
.card:hover{border-color:var(--border-strong)}
.card .num{font-size:30px;font-weight:700;font-variant-numeric:tabular-nums;letter-spacing:-.02em;color:var(--text)}
.card .label{font-size:12px;color:var(--text2);margin-top:5px;display:flex;align-items:center;gap:6px}
.card .label::before{content:'';width:7px;height:7px;border-radius:50%;background:var(--lab-c,var(--accent))}
.card .num.green{color:var(--accent2);--lab-c:var(--accent2)}
.card .num.red{color:var(--danger);--lab-c:var(--danger)}
.card .num.yellow{color:var(--amber);--lab-c:var(--amber)}
.card .num.blue{color:var(--accent);--lab-c:var(--accent)}
.cards .card{animation:rise .45s ease both}
.cards .card:nth-child(1){animation-delay:.02s}
.cards .card:nth-child(2){animation-delay:.08s}
.cards .card:nth-child(3){animation-delay:.14s}
.cards .card:nth-child(4){animation-delay:.2s}
@keyframes rise{from{opacity:0;transform:translateY(10px)}to{opacity:1;transform:none}}

/* ===== Sections ===== */
.section{background:var(--panel);border:1px solid var(--border);border-radius:var(--radius);margin-bottom:22px;overflow:hidden;animation:rise .4s ease both}
.section-title{padding:13px 18px;border-bottom:1px solid var(--border);font-weight:600;font-size:14px;display:flex;align-items:center;gap:8px}
.section-title .sec-ico{width:17px;height:17px;display:inline-flex;align-items:center;justify-content:center;flex-shrink:0}
.section-title .sec-ico svg{width:15px;height:15px;stroke:var(--text2);fill:none;stroke-width:1.7;stroke-linecap:round;stroke-linejoin:round}
.section-body{padding:18px}
.tabs{display:flex;border-bottom:1px solid var(--border);padding:0 8px;gap:4px;overflow-x:auto}
.tab{padding:11px 18px;cursor:pointer;color:var(--text2);border-bottom:2px solid transparent;font-size:13px;white-space:nowrap;transition:.15s}
.tab:hover{color:var(--text);background:rgba(255,255,255,.04)}
.tab.active{color:var(--text);border-bottom-color:var(--accent);font-weight:600}
.tab-content{display:none;padding:18px}
.tab-content.active{display:block;animation:rise .25s ease both}

/* ===== Tables ===== */
.table-wrap{overflow-x:auto}
table{width:100%;border-collapse:collapse}
th,td{text-align:left;padding:10px 14px;border-bottom:1px solid var(--border);font-size:13px;white-space:nowrap}
th{color:var(--text3);font-weight:600;font-size:11.5px;text-transform:uppercase;letter-spacing:.06em}
tbody tr{transition:.12s}
tbody tr:hover{background:rgba(255,255,255,.035)}
tbody tr:last-child td{border-bottom:none}

/* ===== Status badges ===== */
.status{display:inline-flex;align-items:center;gap:6px;padding:3px 10px;border-radius:999px;font-size:12px;font-weight:600}
.status.active{background:var(--status-active-bg);color:var(--accent2)}
.status.cooldown{background:var(--status-cooldown-bg);color:var(--amber)}
.status.expired{background:var(--status-expired-bg);color:var(--danger)}
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
.btn-sm svg{width:13px;height:13px}

/* ===== Forms ===== */
input,textarea,select{width:100%;padding:9px 13px;background:rgba(0,0,0,.25);border:1px solid var(--border);border-radius:var(--radius-sm);color:var(--text);font-size:13px;font-family:inherit;transition:.15s}
[data-theme="light"] input,[data-theme="light"] textarea,[data-theme="light"] select{background:#fff}
input::placeholder,textarea::placeholder{color:var(--text3)}
input:focus,textarea:focus,select:focus{outline:none;border-color:var(--accent);box-shadow:0 0 0 3px rgba(138,180,248,.12)}
textarea{resize:vertical;min-height:84px;font-family:'JetBrains Mono','Cascadia Code',Consolas,monospace;font-size:12px}
select{cursor:pointer;appearance:none;background-image:linear-gradient(45deg,transparent 50%,var(--text2) 50%),linear-gradient(135deg,var(--text2) 50%,transparent 50%);background-position:calc(100% - 18px) 55%,calc(100% - 13px) 55%;background-size:5px 5px;background-repeat:no-repeat;padding-right:32px}
.form-row{display:flex;gap:14px;align-items:flex-end;margin-bottom:14px;flex-wrap:wrap}
.form-row .field{flex:1;min-width:180px}
.form-row .field label{display:block;font-size:12px;color:var(--text2);margin-bottom:6px;font-weight:500}
.form-actions{display:flex;gap:10px;margin-top:14px;flex-wrap:wrap}
/* 出口绑定联想下拉（combobox） */
.eg-input{width:170px;font-size:12px;padding:5px 8px;border-radius:7px;border:1px solid var(--border-strong);background:var(--bg3);color:var(--text);cursor:pointer}
.eg-input:focus{outline:none;border-color:var(--accent)}
.eg-menu{position:absolute;z-index:1200;background:var(--bg2);border:1px solid var(--border-strong);border-radius:8px;box-shadow:0 8px 24px rgba(0,0,0,.35);max-height:260px;overflow-y:auto;min-width:220px}
.eg-item{padding:6px 10px;font-size:12px;cursor:pointer;color:var(--text);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.eg-item:hover{background:rgba(138,180,248,.15)}
.eg-item.active{background:rgba(138,180,248,.22)}
.eg-item.eg-hint{cursor:default;color:var(--text3)}
.eg-item.eg-hint:hover{background:none}
.flex{display:flex;align-items:center;gap:8px}
.gap-4{gap:4px}
.text-right{text-align:right}
.mt-8{margin-top:8px}
.inline-flex{display:inline-flex;align-items:center;gap:6px}
.justify-between{display:flex;justify-content:space-between;align-items:center;gap:10px;flex-wrap:wrap}
.hint{font-size:12px;color:var(--text2);margin-top:8px;line-height:1.6}
.hint strong{color:var(--text)}

/* ===== Dashboard: quick start & endpoints ===== */
.steps{display:flex;flex-direction:column;gap:0}
.step{display:flex;gap:14px;position:relative;padding-bottom:18px}
.step:last-child{padding-bottom:0}
.step::before{content:'';position:absolute;left:13px;top:30px;bottom:2px;width:2px;background:var(--border);border-radius:2px}
.step:last-child::before{display:none}
.step-no{width:27px;height:27px;flex-shrink:0;border-radius:50%;border:1px solid var(--border-strong);color:var(--text2);font-weight:600;font-size:12.5px;display:flex;align-items:center;justify-content:center;background:rgba(255,255,255,.03)}
.step-title{font-weight:600;font-size:13.5px;margin-top:3px}
.step-desc{font-size:12.5px;color:var(--text2);margin-top:3px;line-height:1.6}
.step-desc code{background:rgba(255,255,255,.07);padding:1px 7px;border-radius:5px}
.endpoint-list{display:flex;flex-direction:column;gap:8px}
.endpoint{display:flex;align-items:center;gap:12px;padding:9px 13px;background:rgba(255,255,255,.025);border:1px solid var(--border);border-radius:8px;flex-wrap:wrap}
.endpoint code{background:rgba(138,180,248,.08);border:1px solid rgba(138,180,248,.2);color:var(--accent);padding:3px 10px;border-radius:6px;font-size:11.5px;white-space:nowrap}
.endpoint span{font-size:12px;color:var(--text2);flex:1;min-width:160px}
.danger-zone{border-color:rgba(224,112,95,.28)}
.danger-zone .section-title{color:var(--danger)}
.auto-pill{font-size:11px;color:var(--text2);background:rgba(255,255,255,.05);border:1px solid var(--border);border-radius:999px;padding:2px 10px;white-space:nowrap}
.auto-pill.paused{color:var(--text3);border-color:var(--border)}

/* ===== Toast ===== */
.toast{position:fixed;top:22px;right:22px;padding:12px 20px;border-radius:12px;color:#fff;z-index:9999;opacity:0;transform:translateY(-12px) scale(.97);transition:.3s cubic-bezier(.2,.9,.3,1.2);font-size:13px;max-width:420px;backdrop-filter:blur(12px);border:1px solid rgba(255,255,255,.14);box-shadow:0 12px 40px rgba(2,6,23,.5);white-space:pre-line}
.toast.show{opacity:1;transform:none}
.toast.success{background:#2b5a44}
.toast.error{background:#7a3a30}
.toast.info{background:#31465e}
.toast.warning{background:#6e5619}

/* ===== Misc ===== */
.loading{display:inline-block;width:14px;height:14px;border:2px solid var(--text3);border-top-color:var(--accent);border-radius:50%;animation:spin .7s linear infinite;vertical-align:-2px}
@keyframes spin{to{transform:rotate(360deg)}}
.empty{padding:30px;text-align:center;color:var(--text2)}
.empty-state{padding:44px 20px;text-align:center;color:var(--text2)}
.empty-state .icon{font-size:40px;margin-bottom:10px;display:block;opacity:.8}
.key-display{background:rgba(0,0,0,.3);padding:9px 13px;border-radius:var(--radius-sm);border:1px solid var(--border);font-family:'JetBrains Mono','Cascadia Code',Consolas,monospace;font-size:12px;word-break:break-all;cursor:pointer;transition:.15s}
.key-display:hover{border-color:var(--accent)}
.copy-icon{cursor:pointer;color:var(--text2);padding:2px 6px;border-radius:4px}
.copy-icon:hover{color:var(--text);background:var(--bg3)}
.model-tag{display:inline-block;padding:2px 9px;border-radius:6px;font-size:11px;background:rgba(255,255,255,.06);color:var(--text2);margin:2px;letter-spacing:.02em}
.model-tag.free{border:1px solid rgba(126,226,168,.35);color:var(--accent2);background:rgba(126,226,168,.07)}
.model-tag.pass{border:1px solid rgba(217,176,74,.35);color:var(--amber);background:rgba(217,176,74,.07)}
.theme-toggle{display:inline-flex;align-items:center;gap:5px;padding:4px 9px;border:1px solid var(--border);border-radius:7px;background:rgba(255,255,255,.04);color:var(--text2);cursor:pointer;font-size:12px;transition:.15s;font-family:inherit}
.theme-toggle:hover{color:var(--text);background:rgba(255,255,255,.09)}
[data-theme="dark"] .theme-toggle .light-label{display:none}
body:not([data-theme="dark"]) .theme-toggle .dark-label{display:none}
.probe-pill{font-size:11px;color:var(--text3)}
.oauth-card{border:1px solid var(--border);border-radius:10px;padding:16px;background:rgba(255,255,255,.03);margin-top:14px}
.stat-mini{font-family:'JetBrains Mono',Consolas,monospace;font-size:12px;color:var(--text2)}

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
}
@media (max-width:560px){
  .cards{grid-template-columns:repeat(2,1fr)}
  h2{font-size:18px}
  .section-title{padding:11px 14px}
  .section-body{padding:14px}
  .form-row .field{min-width:100%}
}
</style>
</head>
<body>
<div class="layout">
<div class="sidebar">
<h1><span class="logo"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M13 2 3 14h7l-1 8 10-12h-7l1-8z"/></svg></span><span class="brand-name">AFree Proxy</span><button class="theme-toggle" id="langBtn" onclick="toggleLang()" title="Switch language / 切换语言" style="margin-left:6px"></button><button class="theme-toggle" onclick="toggleTheme()" title="Toggle theme"><span class="icon" id="themeIcon"></span><span class="light-label">Light</span><span class="dark-label">Dark</span></button></h1>
<div class="nav-item active" data-tab="dashboard"><span class="nav-ico"><svg viewBox="0 0 24 24"><rect x="3" y="3" width="7" height="9" rx="1.5"/><rect x="14" y="3" width="7" height="5" rx="1.5"/><rect x="14" y="12" width="7" height="9" rx="1.5"/><rect x="3" y="16" width="7" height="5" rx="1.5"/></svg></span> Dashboard</div>
<div class="nav-group">Account pool</div>
<div class="nav-item" data-tab="accounts"><span class="nav-ico"><svg viewBox="0 0 24 24"><circle cx="12" cy="8" r="4"/><path d="M4 21c0-4 3.6-6.5 8-6.5s8 2.5 8 6.5"/></svg></span> Cline</div>
<div class="nav-item" data-tab="opencode"><span class="nav-ico"><svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a14.5 14.5 0 0 1 0 18 14.5 14.5 0 0 1 0-18z"/></svg></span> OpenCode</div>
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
<div class="cards">
  <div class="card"><div class="num blue" id="statTotal">-</div><div class="label">Total accounts</div></div>
  <div class="card"><div class="num green" id="statActive">-</div><div class="label">Active</div></div>
  <div class="card"><div class="num yellow" id="statCooldown">-</div><div class="label">Cooldown</div></div>
  <div class="card"><div class="num red" id="statExpired">-</div><div class="label">Expired</div></div>
</div>
<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M13 2 3 14h7l-1 8 10-12h-7l1-8z"/></svg></span> Quick actions</div>
  <div class="section-body" style="display:flex;gap:10px;flex-wrap:wrap">
    <button class="btn btn-primary" onclick="switchTab('import')">Add account</button>
    <button class="btn" onclick="refreshAllTokens()">Refresh all tokens</button>
    <button class="btn" onclick="document.getElementById('fileInput').click()">Import from file</button>
    <input type="file" id="fileInput" accept=".json,.txt" style="display:none" onchange="handleFileImport(event)">
    <button class="btn" onclick="switchTab('settings');generateKey()">Generate API key</button>
  </div>
</div>
<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20"/><path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z"/></svg></span> Quick start</div>
  <div class="section-body">
    <div class="steps">
      <div class="step"><div class="step-no">1</div><div class="step-body"><div class="step-title">Add Cline accounts</div><div class="step-desc">On the Add accounts page: browser OAuth, a refreshToken, or a static API key (<code>sk_...</code>) — accounts are the proxy's upstream quota.</div></div></div>
      <div class="step"><div class="step-no">2</div><div class="step-body"><div class="step-title">Generate an API key</div><div class="step-desc">Generate a key under Proxy settings → API keys to authenticate clients; with no keys configured, unauthenticated access is allowed.</div></div></div>
      <div class="step"><div class="step-no">3</div><div class="step-body"><div class="step-title">Configure your client</div><div class="step-desc">Set the Base URL to <code id="quickstartApi">http://127.0.0.1:3457/v1</code> and pick any model ID from the available models list.</div></div></div>
    </div>
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
  <h2>Cline</h2>
  <div style="display:flex;gap:8px">
    <button class="btn btn-sm" onclick="assignProxiesEvenly()" title="Distribute the proxy pool across all accounts: main = pool[i%N], backup = pool[(i+1)%N]">Assign proxies evenly</button>
    <button class="btn btn-sm" onclick="clearAccountProxies()" title="Clear proxy bindings on ALL accounts — every account returns to the Global default">Clear bindings</button>
    <button class="btn btn-sm" onclick="exportAccounts()">Export accounts</button>
    <button class="btn btn-primary btn-sm" onclick="switchTab('import')">Add</button>
    <button class="btn btn-sm" onclick="loadAccounts()">Refresh</button>
  </div>
</div>
<div class="hint" style="margin:-4px 0 16px;padding:11px 14px;border:1px solid var(--border);border-radius:10px;background:rgba(148,163,184,.05)">
  <strong style="color:var(--text)">Tokens</strong> are counted locally by this proxy (input+output; exact when upstream returns usage, otherwise estimated from the request body) to gauge headroom before official rate limits. The Test button sends a real probe request. The Reset button <strong style="color:var(--text)">probes upstream rate-limit status</strong>: if still limited it stays in cooldown and shows the estimated recovery time; only a passing probe lifts the cooldown and resets today's stats.
  <strong style="color:var(--text)">Proxy binding</strong> (isolation mode, on by default): a bound account always egresses via its main proxy, falls back to the backup, and is <em>skipped entirely</em> while both are unavailable — never routed through another exit. Configure the list on the <a href="#" onclick="switchTab('proxypool');return false" style="color:var(--accent);cursor:pointer">Proxy pool</a> page.
</div>
<div class="section">
  <div class="section-body" style="padding:6px">
    <div class="table-wrap">
    <table>
      <thead>
        <tr><th>Email</th><th>Status</th><th title="Counted locally by this proxy — not the official free quota">Today/Total tokens</th><th>Last used</th><th>Created</th><th title="Main / backup egress — isolation mode picks main first, then backup; both down = account skipped">Proxy binding</th><th>Actions</th></tr>
      </thead>
      <tbody id="accountTableBody">
        <tr><td colspan="7" class="empty">Loading...</td></tr>
      </tbody>
    </table>
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
    <div class="hint">These headers are attached to every request forwarded to the Cline API to mimic the official client.</div>
    <div id="headerSaveResult" style="margin-top:8px"></div>
  </div>
</div>
</div>

<div id="tab-import" class="tab-panel" style="display:none">
<h2>Add accounts</h2>
<div class="section">
  <div class="tabs" id="importTabs">
    <div class="tab active" data-tab="oauth">OAuth browser login</div>
    <div class="tab" data-tab="token">Manual entry</div>
    <div class="tab" data-tab="batch">Batch import</div>
  </div>

  <div id="import-oauth" class="tab-content active">
    <p class="hint">Cline accounts via browser OAuth — Google/GitHub/email sign-in supported; the refreshToken is captured automatically.</p>
    <div class="form-actions">
      <button class="btn btn-primary" onclick="startOAuth()" id="oauthBtn">Start OAuth login</button>
    </div>
    <div id="oauthProgress" style="display:none;margin-top:14px" class="oauth-card">
      <div style="display:flex;align-items:center;gap:14px">
        <div class="loading"></div>
        <div>
          <div style="font-weight:600" id="oauthStatus">Waiting for browser authorization...</div>
          <div class="hint">
            Open <a href="#" id="oauthUrl" target="_blank" style="color:var(--accent)"></a>
            and enter the code: <strong style="color:var(--accent);font-size:15px;letter-spacing:2px" id="oauthUserCode"></strong>
          </div>
        </div>
      </div>
    </div>
    <div id="oauthResult" style="display:none;margin-top:14px"></div>
  </div>

  <div id="import-token" class="tab-content">
    <p class="hint">Paste a Cline OAuth refreshToken or a static Cline API key (<code>sk_...</code>) — the type is detected automatically. API keys are used as-is (no validation call); refresh tokens are verified against the upstream first.</p>
    <div class="form-row">
      <div class="field">
        <label>Token or API key *</label>
        <input type="text" id="tokenInput" placeholder="workos-... refreshToken or sk_... API key" style="font-family:'JetBrains Mono',Consolas,monospace">
      </div>
      <div class="field">
        <label>Email / label (optional; auto-generated if empty)</label>
        <input type="text" id="tokenEmail" placeholder="user@example.com">
      </div>
    </div>
    <div class="form-actions">
      <button class="btn btn-primary" onclick="addByToken()">Add account</button>
    </div>
    <div id="tokenResult" style="margin-top:8px"></div>
  </div>

  <div id="import-batch" class="tab-content">
    <p class="hint">Import multiple accounts at once. Accepts a JSON array (entries with <code>refreshToken</code> or <code>apiToken</code>) or one credential per line — lines starting with <code>sk_</code> are treated as API keys, everything else as refresh tokens.</p>
    <div class="form-row">
      <div class="field">
        <label>JSON array or one credential per line</label>
        <textarea id="batchInput" placeholder='[{"refreshToken":"xxx","email":"u1@x.com"},{"apiToken":"sk_yyy","email":"u2@x.com"}]'></textarea>
      </div>
    </div>
    <div class="form-actions">
      <button class="btn btn-primary" onclick="batchImport()">Import all</button>
      <button class="btn" onclick="document.getElementById('fileInput2').click()">Choose file</button>
      <input type="file" id="fileInput2" accept=".json,.txt" style="display:none" onchange="handleFileImport(event)">
    </div>
    <div id="batchResult" style="margin-top:8px"></div>
  </div>
</div>
</div>

<div id="tab-proxypool" class="tab-panel" style="display:none">
<h2>Proxy pool</h2>
<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><circle cx="12" cy="5" r="2.5"/><circle cx="5" cy="19" r="2.5"/><circle cx="19" cy="19" r="2.5"/><path d="M12 7.5v4m0 0-5.5 5m5.5-5 5.5 5"/></svg></span> Egress proxies</div>
  <div class="section-body">
    <div class="hint" style="margin-bottom:10px">One proxy list serves every upstream that opts in below. One proxy per line: <code>http://user:pass@host:port</code> or <code>socks5://host:port</code>. Requests rotate across healthy proxies per request; a proxy that hits a rate limit is cooled down and skipped automatically.</div>
    <div class="form-row">
      <div class="field" style="flex:3"><label>Proxy list</label>
        <textarea id="ppProxies" rows="4" placeholder="one per line: socks://b64(user:pass)@host:port#alias, socks5://user:pass@host:port#alias or http://host:port"></textarea>
      </div>
      <div class="field"><label>Proxy selection strategy</label>
        <select id="ppStrategy"><option value="round_robin">Round-robin (round_robin)</option><option value="random">Random (random)</option><option value="fill">Fill (fill)</option></select>
      </div>
    </div>
    <div class="form-actions"><button class="btn btn-primary" onclick="saveProxyPool()">Save proxy pool</button></div>
    <div id="ppSaveResult" style="margin-top:8px"></div>
  </div>
</div>
<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M12 5v14M5 12h14"/></svg></span> Global policy</div>
  <div class="section-body">
    <div style="display:grid;grid-template-columns:1fr 1fr;gap:10px 24px;align-items:center">
      <div class="field" style="margin:0"><label style="margin:0">Cline</label></div>
      <div class="field" style="margin:0"><label style="margin:0">OpenCode</label></div>
      <div class="field" style="margin:0">
        <div style="display:flex;gap:6px;align-items:center">
          <select id="ppCline" style="flex:1"><option value="true">Proxy</option><option value="false">Direct connection</option></select>
          <button class="btn btn-sm btn-primary" onclick="saveClineProxies()">Save</button>
        </div>
      </div>
      <div class="field" style="margin:0">
        <div style="display:flex;gap:6px;align-items:center">
          <select id="ppZen" style="flex:1"><option value="true">Proxy</option><option value="false">Direct connection</option></select>
          <button class="btn btn-sm btn-primary" onclick="saveZenProxies()">Save</button>
        </div>
      </div>
      <div class="hint" style="margin:0">Off (direct) by default. The <code>CLINE_USE_PROXIES=true</code> env var always forces this on.</div>
      <div class="hint" style="margin:0">OpenCode keys without proxy bindings follow this switch.</div>
    </div>
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
          <button class="btn btn-sm btn-primary" onclick="saveProxyIsolation()">Save</button>
        </div>
      </div>
    </div>
    <div class="hint">With isolation on, a Cline account or OpenCode key bound to a main/backup proxy always egresses via the main, falls back to the backup, and is <b>skipped entirely</b> while both are cooling or removed — it never leaks through another exit or a direct connection. The OpenCode harvester also mints sessions through the key's bound exit so a session never changes IP. Bind per account on the Cline page, per key on the OpenCode page, or use "Assign proxies evenly" there. <code>PROXY_ISOLATION</code> env var overrides this switch when set.</div>
    <div class="hint" id="ppIsolationHint" style="margin-top:4px"></div>
  </div>
</div>
<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="9"/><path d="M12 8v4m0 4h.01"/></svg></span> Cooldown status</div>
  <div class="section-body">
    <div class="hint" id="ppCooldownInfo" style="margin:0">-</div>
  </div>
</div>
</div>

<div id="tab-settings" class="tab-panel" style="display:none">
<h2>Gateway settings</h2>

<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M21 2l-2 2m-7.6 7.6a5.5 5.5 0 1 1-7.78 7.78 5.5 5.5 0 0 1 7.78-7.78zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4"/></svg></span> API keys</div>
  <div class="section-body">
    <p class="hint">Generated keys authenticate client access to the proxy API (sent as the x-api-key or Authorization header).</p>
    <div class="form-actions" style="margin-bottom:14px">
      <button class="btn btn-success" onclick="generateKey()">Generate new key</button>
    </div>
    <div id="keysList"></div>
    <div id="keyGenResult" style="margin-top:8px"></div>
  </div>
</div>

<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><rect x="2" y="7" width="20" height="14" rx="2"/><path d="M16 7V5a2 2 0 0 0-2-2h-4a2 2 0 0 0-2 2v2"/><path d="M12 12v3M8.5 13.5v0M15.5 13.5v0"/></svg></span> Available models <span id="modelsProbeInfo" class="probe-pill" style="font-weight:normal"></span></div>
  <div class="section-body">
    <div class="flex" style="margin-bottom:10px;gap:10px">
      <button class="btn btn-sm btn-primary" onclick="refreshModels()">Refresh models</button>
      <span class="hint" style="margin:0">Auto-syncs official free-model feeds for both upstreams (60s); only zero-cost models are shown</span>
    </div>
    <div id="modelsList">Loading...</div>
  </div>
</div>

<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="3"/><path d="M12 2v4m0 12v4M2 12h4m12 0h4M4.9 4.9l2.8 2.8m8.6 8.6 2.8 2.8m0-14.2-2.8 2.8M7.7 16.3l-2.8 2.8"/></svg></span> General config</div>
  <div class="section-body">
    <div class="form-row">
      <div class="field"><label>Listen address</label><input type="text" id="settingAddr" disabled></div>
      <div class="field">
        <label>Default model</label>
        <div style="display:flex;gap:6px;align-items:center">
          <select id="settingDefModel" style="flex:1;font-family:'JetBrains Mono',Consolas,monospace"></select>
          <button class="btn btn-sm btn-primary" onclick="saveDefaultModel()">Save</button>
        </div>
      </div>
    </div>
    <div class="form-row">
      <div class="field">
        <label>Account scheduling strategy</label>
        <select id="settingStrategy" onchange="updateConfig()">
          <option value="round_robin">Round-robin (round_robin)</option>
          <option value="random">Random (random)</option>
          <option value="fill">Fill (fill)</option>
        </select>
      </div>
      <div class="field"><label>Engine version</label><input type="text" id="settingVersion" disabled></div>
    </div>
  </div>
</div>

<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M12 3v12m0 0 4-4m-4 4-4-4"/><path d="M4 17v2a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-2"/></svg></span> Config export / import</div>
  <div class="section-body">
    <div class="hint">Exports/imports everything persisted: Cline accounts, client keys, OpenCode keys (with enabled/bindings), the proxy pool (with aliases), request headers, scheduling strategy, custom aliases, zen sessions and learned endpoints. <b>The file contains ALL secrets — keep it safe.</b> Import replaces sections present in the file; sections absent from the file are left untouched.</div>
    <div class="form-actions">
      <button class="btn btn-success" onclick="exportConfig()">Export config</button>
      <button class="btn" onclick="document.getElementById('importFile').click()">Choose backup file</button>
      <button class="btn btn-primary" id="importBtn" disabled onclick="importConfigFromFile()">Import config</button>
      <input type="file" id="importFile" accept=".json,application/json" style="display:none" onchange="onImportFileChange(event)">
    </div>
    <div id="importResult" style="margin-top:8px"></div>
  </div>
</div>

<div class="section danger-zone">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M3 6h18M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2m3 0v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"/></svg></span> Danger zone</div>
  <div class="section-body">
    <div style="display:flex;gap:10px;flex-wrap:wrap">
      <button class="btn btn-danger" onclick="deleteAllAccounts()">Delete all accounts</button>
      <button class="btn btn-danger" onclick="deleteAllKeys()">Delete all keys</button>
    </div>
  </div>
</div>
</div>

<div id="tab-logs" class="tab-panel" style="display:none">
<div class="flex justify-between" style="margin-bottom:16px">
  <h2>Request logs <span class="probe-pill" style="font-weight:normal">last 500 entries, persisted to data/requests.jsonl</span></h2>
  <div style="display:flex;gap:8px;align-items:center">
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
        <tr><th>Time</th><th>Source</th><th>Method</th><th>Path</th><th>Model</th><th>Route</th><th>Upstream</th><th>Status</th><th>Duration</th></tr>
      </thead>
      <tbody id="logsTableBody">
        <tr><td colspan="9" class="empty">Loading...</td></tr>
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
    <div class="hint" style="margin-bottom:10px">Clients request the alias ID as their model, and the proxy rewrites it to the target model for the chosen platform. Alias IDs are user-defined (e.g. <code>cline-glm-5.3</code>) and must not collide with real model IDs; the target must belong to the same platform — cross-platform targets are not allowed.</div>
    <div class="form-row">
      <div class="field"><label>Alias ID</label><input type="text" id="comboId" placeholder="cline-glm-5.3"></div>
      <div class="field"><label>Platform</label>
        <select id="comboPlatform" onchange="fillComboModels()"><option value="cline">cline</option><option value="zen">opencode</option></select>
      </div>
      <div class="field" style="flex:2"><label>Target model (same platform only)</label><select id="comboTarget"></select></div>
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
<h2>OpenCode</h2>

<div class="section">
  <div class="section-body">
    <div class="form-row">
      <div class="field" style="flex:1"><label>API keys (one per line, round-robin rotation, auto-cooldown on 429)</label><textarea id="ocKeys" rows="3" placeholder="public"></textarea></div>
    </div>
    <div class="flex" style="gap:10px;margin-bottom:8px;align-items:center;flex-wrap:wrap">
      <label class="hint" style="margin:0">Probe model (used by the Test buttons):</label>
      <select id="ocProbeModel" style="max-width:360px"><option value="">auto — big-pickle first, then live models</option></select>
    </div>
    <div style="display:flex;justify-content:flex-end;margin-bottom:8px">
      <button class="btn btn-sm" onclick="enableAllZenKeys(true)" title="Enable auto-minting on ALL keys">Enable all</button>
      <button class="btn btn-sm" onclick="clearZenKeyProxies()" title="Clear proxy bindings on ALL keys — every key returns to the Global default">Clear bindings</button>
    </div>
    <div class="table-wrap" style="margin-bottom:10px">
      <table>
        <thead><tr><th style="width:50px">#</th><th style="width:110px">Key</th><th style="width:70px">Usage</th><th style="width:110px">Session</th><th>Cooldown</th><th title="Main / backup egress — isolation mode picks main first, then backup; both down = key skipped. The harvester also mints through the bound exit.">Proxy binding</th><th style="width:90px"></th><th title="Auto-mint participation: unchecked keys never trigger session minting (startup / 403 / periodic / manual Mint)">Enabled</th></tr></thead>
        <tbody id="ocKeysBody"><tr><td colspan="8" class="empty">Loading...</td></tr></tbody>
      </table>
    </div>
    <div class="form-row">
      <div class="field"><label>Base URL</label><input type="text" id="ocBaseURL" placeholder="https://opencode.ai/zen/v1"></div>
    </div>
    <div class="form-actions"><button class="btn btn-primary" onclick="saveOcConfig()">Save config</button></div>
  </div>
</div>

<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg></span> Rate-limit defense</div>
  <div class="section-body">
    <div class="form-row">
      <div class="field"><label>Max concurrency</label><input type="text" id="ocMaxConc" placeholder="8"></div>
      <div class="field"><label>Rate-limit retries</label><input type="text" id="ocRetries" placeholder="3"></div>
    </div>
    <div class="form-row">
      <div class="field"><label>Failover</label>
        <select id="ocFailover"><option value="true">On (switch to cline pool)</option><option value="false">Off</option></select>
      </div>
      <div class="field"><label>Failure threshold</label><input type="text" id="ocFailoverCount" placeholder="3"></div>
    </div>
    <div class="form-row">
      <div class="field"><label>Failover window (min)</label><input type="text" id="ocFailoverMinutes" placeholder="5"></div>
      <div class="field"><label>Current status</label><span id="ocFailoverInfo" class="stat-mini" style="align-self:center">-</span></div>
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
    <p class="hint" style="margin-top:0" id="ocSessHint">The free tier only accepts session IDs the upstream has actually seen, minted by the opencode CLI. A key without a live session <b>always</b> fails with 403 — normally the harvester mints one on startup, on repeated 403s, and every few hours; use the buttons below to mint immediately (e.g. right after a fresh deploy with many keys).</p>
    <div class="form-actions" style="margin-bottom:10px;align-items:center">
      <button class="btn btn-primary" id="ocSessBtnMissing" onclick="mintZenSessions(false)">Mint missing sessions</button>
      <button class="btn" id="ocSessBtnForce" onclick="mintZenSessions(true)">Force mint / refresh all</button>
      <span class="hint" style="margin:0" id="ocSessTimer"></span>
    </div>
    <div class="table-wrap">
      <table>
        <thead><tr><th style="width:70px">Key</th><th style="width:130px">Session</th><th style="width:150px">State</th><th>Last minted</th></tr></thead>
        <tbody id="ocSessBody"><tr><td colspan="4" class="empty">Loading...</td></tr></tbody>
      </table>
    </div>
    <div id="ocSessResult" style="margin-top:10px"></div>
  </div>
</div>

<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M3 3v18h18"/><path d="M7 15l4-4 3 3 5-6"/></svg></span> OpenCode stats</div>
  <div class="section-body">
    <div class="table-wrap"><div id="ocStatsBox"></div></div>
    <div id="ocModelStatsBox" style="margin-top:14px"></div>
  </div>
</div>
</div>

</div>
</div>

<div id="toast" class="toast"></div>

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
const fmtTokens = n => {
  n = n || 0;
  if (n >= 1000000) return (n / 1000000).toFixed(2).replace(/\.?0+$/, '') + 'M';
  if (n >= 1000) return (n / 1000).toFixed(1).replace(/\.0$/, '') + 'K';
  return String(n);
};
// ========== i18n（English / 简体中文） ==========
// 机制：字典以英文原文为键。静态 HTML 与 JS 渲染出的纯文本节点由
// MutationObserver 即时翻译（渲染代码无需逐处包翻译函数）；与变量拼接的
// 字符串在渲染处用 T() 包住英文片段；confirm() 原生对话框只能显式 T()。
// 切换语言 = 保存 localStorage 偏好 + location.reload()（避免维护反向映射，
// 也保证重载后静态文本从首次渲染起就是目标语言）。
let LANG = localStorage.getItem('lang') === 'zh' ? 'zh' : 'en';
const I18N_ZH = {
  // 侧边导航 / 分组
  'Account pool': '账号池',
  'Services': '服务',
  'Dashboard': '仪表盘',
  'Accounts': 'Cline',
  'Add accounts': '添加账号',
  'Gateway settings': '网关设置',
  'Request logs': '请求日志',
  'Proxy pool': '代理池',
  'opencode free models': 'opencode',
  'Custom aliases': '自定义别名',
  'Light': '亮色',
  'Dark': '暗色',
  'Toggle theme': '切换主题',
  // 仪表盘
  'Total accounts': '账号总数',
  'Active': '活跃',
  'Cooldown': '冷却中',
  'Expired': '已失效',
  'Quick actions': '快捷操作',
  'Add account': '添加账号',
  'Refresh all tokens': '刷新全部 token',
  'Import from file': '从文件导入',
  'Generate API key': '生成 API key',
  'Quick start': '快速上手',
  'Add Cline accounts': '添加 Cline 账号',
  'Generate an API key': '生成 API key',
  'Configure your client': '配置客户端',
  'Endpoints': '接口',
  'OpenAI Chat format — works with most tools out of the box': 'OpenAI Chat 格式 —— 大多数工具开箱即用',
  'Anthropic Messages format — for Claude Code / Cline and similar clients': 'Anthropic Messages 格式 —— 供 Claude Code / Cline 等客户端使用',
  'OpenAI Responses format — for Cursor and similar clients': 'OpenAI Responses 格式 —— 供 Cursor 等客户端使用',
  'Model list': '模型列表',
  // 快速上手（含 <code> 拆分的文本片段）
  'On the Add accounts page: browser OAuth, a refreshToken, or a static API key (': '在「添加账号」页操作：浏览器 OAuth、refreshToken 或静态 API key（',
  ") — accounts are the proxy's upstream quota.": '）—— 账号就是代理的上游额度。',
  'Generate a key under Proxy settings → API keys to authenticate clients; with no keys configured, unauthenticated access is allowed.': '在 网关设置 → API key 下生成 key 供客户端鉴权；未配置任何 key 时允许匿名访问。',
  'Set the Base URL to': '把 Base URL 设为 ',
  'and pick any model ID from the available models list.': '，并从可用模型列表里任选一个模型 ID。',
  // 账号页
  'Assign proxies evenly': '均匀分配代理',
  'Export accounts': '导出账号',
  'Add': '添加',
  'Refresh': '刷新',
  'Email': '邮箱',
  'Status': '状态',
  'Today/Total tokens': '今日/累计 token',
  'Last used': '最近使用',
  'Created': '创建时间',
  'Proxy binding': '代理绑定',
  'Actions': '操作',
  'Loading...': '加载中...',
  'are counted locally by this proxy (input+output; exact when upstream returns usage, otherwise estimated from the request body) to gauge headroom before official rate limits. The Test button sends a real probe request. The Reset button': '由本代理本地统计（输入+输出；上游返回 usage 时精确，否则按请求体估算），用于判断距离官方限流还有多少余量。Test 按钮发送一次真实探测请求。Reset 按钮 ',
  'probes upstream rate-limit status': '探测上游限流状态',
  ": if still limited it stays in cooldown and shows the estimated recovery time; only a passing probe lifts the cooldown and resets today's stats.": '：若仍在限流则保持冷却并显示预计恢复时间；只有探测通过才会解除冷却并重置今日统计。',
  '(isolation mode, on by default): a bound account always egresses via its main proxy, falls back to the backup, and is': '（隔离模式，默认开启）：绑定的账号永远从主代理出网，主不可用落到辅代理；',
  'skipped entirely': '完全跳过',
  'while both are unavailable — never routed through another exit. Configure the list on the': '两者都不可用时整个账号被跳过 —— 绝不路由到其他出口。代理列表在',
  'page.': ' 页配置。',
  'Distribute the proxy pool across all accounts: main = pool[i%N], backup = pool[(i+1)%N]': '把代理池均匀分配给全部账号：主 = 池[i%N]，辅 = 池[(i+1)%N]',
  'Counted locally by this proxy — not the official free quota': '本代理本地统计 —— 非官方免费额度',
  'Main / backup egress — isolation mode picks main first, then backup; both down = account skipped': '主/辅出口 —— 隔离模式先用主、再用辅；双不可用 = 跳过该账号',
  'Main / backup egress — isolation mode picks main first, then backup; both down = key skipped. The harvester also mints through the bound exit.': '主/辅出口 —— 隔离模式先用主、再用辅；双不可用 = 跳过该 key。收割机铸造也走绑定出口。',
  'Main exit — used whenever available': '主出口 —— 可用即优先',
  'Backup exit — used when main is cooling/removed': '辅出口 —— 主冷却/被删时使用',
  'Bound proxy is no longer in the proxy pool — the account is skipped until fixed': '绑定的代理已不在代理池中 —— 修复前该账号一直被跳过',
  'Bound proxy is no longer in the proxy pool — the key is skipped until fixed': '绑定的代理已不在代理池中 —— 修复前该 key 一直被跳过',
  'Test whether the account works (success clears cooldown/expired state)': '测试账号是否可用（成功即清除冷却/失效状态）',
  'Probe the rate limit and lift it: probes upstream; if still limited, stays in cooldown and shows recovery time': '探测限流并尝试解除：探测上游；仍在限流则保持冷却并显示恢复时间',
  'No accounts yet — add one on the': '还没有账号 —— 到',
  // 添加账号页
  'OAuth browser login': 'OAuth 浏览器登录',
  'Manual entry': '手动录入',
  'Batch import': '批量导入',
  'Cline accounts via browser OAuth — Google/GitHub/email sign-in supported; the refreshToken is captured automatically.': '通过浏览器 OAuth 添加 Cline 账号 —— 支持 Google/GitHub/邮箱登录；refreshToken 自动捕获。',
  'Start OAuth login': '开始 OAuth 登录',
  'Waiting for browser authorization...': '等待浏览器授权...',
  'Paste a Cline OAuth refreshToken or a static Cline API key (': '粘贴 Cline OAuth refreshToken 或静态 Cline API key（',
  ') — the type is detected automatically. API keys are used as-is (no validation call); refresh tokens are verified against the upstream first.': '）—— 类型自动识别。API key 直接入库（不做校验请求）；refreshToken 会先向上游验证。',
  'Token or API key *': 'Token 或 API key *',
  'Email / label (optional; auto-generated if empty)': '邮箱 / 标签（可选，留空自动生成）',
  'Import multiple accounts at once. Accepts a JSON array (entries with': '一次导入多个账号。接受 JSON 数组（条目含 ',
  ') or one credential per line — lines starting with': '）或每行一条凭据 —— 以 ',
  'are treated as API keys, everything else as refresh tokens.': ' 开头的行识别为 API key，其余识别为 refreshToken。',
  'JSON array or one credential per line': 'JSON 数组或每行一条凭据',
  'Import all': '全部导入',
  'Choose file': '选择文件',
  // 代理池页
  'Egress proxies': '出口代理',
  'One proxy list serves every upstream that opts in below. One proxy per line:': '一份代理列表供下方所有选择启用的上游共用。每行一条：',
  'or': '或',
  '. Requests rotate across healthy proxies per request; a proxy that hits a rate limit is cooled down and skipped automatically.': '。请求按次在健康代理间轮转；触发限流的代理会自动冷却并被跳过。',
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
  'Restore defaults': '恢复默认',
  'Restore the default Cline CLI headers': '恢复默认的 Cline CLI 请求头',
  'Headers restored to defaults': '请求头已恢复默认',
  'Reset failed': '恢复失败',
  'OpenCode keys without proxy bindings follow this switch.': 'OpenCode 未绑定代理的 key 遵循此开关。',
  'Clear bindings': '一键清空',
  'Enable all': '一键启用',
  'Enable auto-minting on ALL keys?': '启用全部 key 的自动铸造？',
  'Disable auto-minting on ALL keys?': '停用全部 key 的自动铸造？',
  'Bindings cleared': '绑定已清空',
  'Clear failed': '清空失败：',
  'Clear proxy bindings on ALL accounts? Every account returns to the Global default.': '清空全部账号的代理绑定？所有账号回到默认值「全局」。',
  'Clear proxy bindings on ALL keys? Every key returns to the Global default (enabled state is kept).': '清空全部 key 的代理绑定？所有 key 回到默认值「全局」（启用状态保留）。',
  'Clear proxy bindings on ALL keys — every key returns to the Global default': '清空全部 key 的代理绑定 —— 所有 key 回到默认值「全局」',
  'Distribute the proxy pool across all accounts: main = pool[i%N], backup = pool[(i+1)%N]': '把代理池均匀分配给全部账号：主 = 池[i%N]，辅 = 池[(i+1)%N]',
  'Direct connection': '直连',
  'Save': '保存',
  'Off (direct) by default. The': '默认关闭（直连）。',
  'env var always forces this on.': ' 环境变量设置时恒定强制开启。',
  'Delete': '删除',
  'Test': '测试',
  'Reset': '重置',
  'Open': '打开',
  'and enter the code:': '，并输入代码：',
  'Tokens': 'Token 用量',
  'Admin panel:': '管理面板：',
  'API address:': 'API 地址：',
  'OpenCode config saved': 'OpenCode 配置已保存',
  'OpenCode proxy setting saved': 'OpenCode 代理设置已保存',
  'Config export / import': '配置导入导出',
  'Export config': '导出配置',
  'Import config': '导入配置',
  'Choose backup file': '选择备份文件',
  'Config exported': '配置已导出',
  'Config imported': '配置已导入',
  'Unsupported backup version': '不支持的备份版本',
  'ready to import': '已就绪，可导入',
  'Choose a backup JSON file first': '请先选择备份 JSON 文件',
  'Invalid backup file: ': '备份文件无效：',
  'Import replaces sections present in the file (accounts / keys / proxies / combos ...). Continue?': '导入将用文件内容替换当前对应的配置段（Cline 账号 / 客户端 key / OpenCode key / 代理池 / 自定义别名等，文件中存在的段都会替换）。继续？',
  'State': '状态',
  'Global': '全局',
  'Direct': '直连',
  'Enabled': '启用',
  'Participates in auto-minting': '勾选后该 key 参与自动铸造（启动补缺 / 403 触发 / 周期巡检 / 手动 Mint）',
  'Key enabled state saved': '启用状态已保存',
  'Follows the main slot': '跟随主出口',
  'No matching proxies': '没有匹配的代理',
  'OpenCode': 'OpenCode',
  'OpenCode automatically routes through the pool above whenever the list is non-empty — no separate switch needed.': '代理列表非空时 OpenCode 自动经上方代理池路由 —— 无需单独开关。',
  'Proxy isolation (identity ↔ exit binding)': '代理隔离（身份 ↔ 出口绑定）',
  'Isolation mode': '隔离模式',
  'Enabled (default) — bound identities only ever use their bound exits': '启用（默认）—— 绑定的身份只从绑定出口出网',
  'Disabled — ignore bindings; exits rotate per the strategy above (direct when pool off)': '关闭 —— 忽略绑定；出口按上方轮转策略轮换（未启用代理池时直连）',
  'With isolation on, a Cline account or OpenCode key bound to a main/backup proxy always egresses via the main, falls back to the backup, and is': '隔离开启时，绑定了主/辅代理的 Cline 账号或 OpenCode key 永远从主代理出网，主不可用落到辅代理；',
  "while both are cooling or removed — it never leaks through another exit or a direct connection. The OpenCode harvester also mints sessions through the key's bound exit so a session never changes IP. Bind per account on the Cline page, per key on the OpenCode page, or use \"Assign proxies evenly\" there.": '两者都不可用（冷却中/已删除）时该身份会被整体跳过 —— 绝不从其他出口或直连泄漏。OpenCode 收割机也经该 key 的绑定出口铸造会话，保证会话 IP 恒定。逐账号在 Cline 页绑定、逐 key 在 OpenCode 页绑定，或使用那里的「均匀分配代理」。',
  'env var overrides this switch when set.': ' 环境变量设置时覆盖此开关。',
  'PROXY_ISOLATION env is set — it overrides this switch. Unset the env var to control isolation here.': '已设置 PROXY_ISOLATION 环境变量 —— 它覆盖此开关。删除该环境变量才能在此控制隔离。',
  'Cooldown status': '冷却状态',
  'No proxies cooling down': '没有代理在冷却中',
  ' cooldown until ': ' 冷却至 ',
  // 网关设置页
  'API keys': 'API key',
  'Generated keys authenticate client access to the proxy API (sent as the x-api-key or Authorization header).': '生成的 key 用于客户端调用代理 API 鉴权（以 x-api-key 或 Authorization 头发送）。',
  'Generate new key': '生成新 key',
  'Available models': '可用模型',
  'Cline free models': 'Cline 免费模型',
  'OpenCode free models': 'OpenCode 免费模型',
  'Refresh models': '刷新模型',
  'Auto-syncs official free-model feeds for both upstreams (60s); only zero-cost models are shown': '自动同步两个上游的官方免费模型源（60 秒）；仅显示零配额模型',
  'General config': '常规配置',
  'Listen address': '监听地址',
  'Default model': '默认模型',
  'Account scheduling strategy': '账号调度策略',
  'Engine version': '引擎版本',
  'Request headers (mimicking the Cline CLI)': '请求头（模拟 Cline CLI）',
  'Header': '请求头',
  'Value': '值',
  'Add header': '添加请求头',
  'Save headers': '保存请求头',
  'These headers are attached to every request forwarded to the Cline API to mimic the official client.': '这些请求头会附加在每个转发到 Cline API 的请求上，用于模拟官方客户端。',
  'Danger zone': '危险区',
  'Delete all accounts': '删除全部账号',
  'Delete all keys': '删除全部 key',
  'no charge': '免费',
  // 请求日志页
  'last 500 entries, persisted to data/requests.jsonl': '最近 500 条，持久化到 data/requests.jsonl',
  'Auto-refreshing': '自动刷新中',
  'Paused': '已暂停',
  'Pause': '暂停',
  'Resume': '继续',
  'Time': '时间',
  'Source': '来源',
  'Method': '方法',
  'Path': '路径',
  'Model': '模型',
  'Route': '路由',
  'Upstream': '账号/Key',
  'Status': '状态',
  'Duration': '耗时',
  'No requests logged': '暂无请求记录',
  'cline pool': 'cline 池',
  'admin': '管理',
  'meta': '元数据',
  'other': '其他',
  // Combos 页
  'Custom aliases (alias models)': '自定义别名',
  'Create custom alias': '创建自定义别名',
  'Clients request the alias ID as their model, and the proxy rewrites it to the target model for the chosen platform. Alias IDs are user-defined (e.g.': '客户端以别名 ID 作为 model 请求，代理将其改写为所选平台上的目标模型。别名 ID 自定义（如 ',
  ') and must not collide with real model IDs; the target must belong to the same platform — cross-platform targets are not allowed.': '）且不得与真实模型 ID 冲突；目标必须属于同一平台 —— 不允许跨平台。',
  'Alias ID': '别名 ID',
  'Platform': '平台',
  'Target model (same platform only)': '目标模型（仅限同平台）',
  'Existing custom aliases': '现有自定义别名',
  'Target model': '目标模型',
  'No combos yet — create one with the form above.': '还没有自定义别名 —— 用上方表单创建一个。',
  'No models on this platform': '该平台上没有可用模型',
  'Failed to load model list': '模型列表加载失败',
  'socks5 pool': 'socks5 池',
  // opencode 页
  'OpenCode': 'OpenCode',
  'Upstream config': '上游配置',
  'Enable OpenCode upstream': '启用 OpenCode 上游',
  'On': '开',
  'Off': '关',
  'API keys (one per line, round-robin rotation, auto-cooldown on 429)': 'API key（每行一个，round-robin 轮转，429 自动冷却）',
  'Probe model (used by the Test buttons):': '探测模型（Test 按钮使用）：',
  'auto — big-pickle first, then live models': '自动 —— big-pickle 优先，其次 live 模型',
  'Key': 'Key',
  'Usage': '用量',
  'Session': '会话',
  'live': 'live',
  'stale': '已失效',
  'not minted': '未铸造',
  'no key': '无 key',
  'cooling': '冷却中',
  ' · until ': ' · 至 ',
  'next in rotation': '轮转中的下一个',
  'Base URL': 'Base URL',
  'Managed on the': '管理入口在 ',
  'page — shared with the Cline upstream.': ' 页 —— 与 Cline 上游共用。',
  'none configured': '未配置',
  'Save config': '保存配置',
  'Rate-limit defense': '限流防御',
  'Max concurrency': '最大并发',
  'Rate-limit retries': '限流重试次数',
  'Failover': '故障转移',
  'On (switch to cline pool)': '开（转移到 cline 池）',
  'Failure threshold': '失败阈值',
  'Failover window (min)': '故障转移窗口（分钟）',
  'Current status': '当前状态',
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
  'The free tier only accepts session IDs the upstream has actually seen, minted by the opencode CLI. A key without a live session': '免费层只接受上游真实见过的会话 ID（由 opencode CLI 铸造）。没有 live 会话的 key ',
  'always': '必定',
  'fails with 403 — normally the harvester mints one on startup, on repeated 403s, and every few hours; use the buttons below to mint immediately (e.g. right after a fresh deploy with many keys).': ' 会以 403 失败 —— 正常情况下收割机会在启动时、反复 403 时和每隔几小时自动补铸；也可用下方按钮立即铸造（例如刚部署、key 很多时）。',
  'Mint missing sessions': '铸造缺失的会话',
  'Force mint / refresh all': '强制重铸全部',
  'opencode CLI not available in this container': '本容器内没有 opencode CLI',
  'Last minted': '上次铸造',
  'OpenCode model list': 'OpenCode 模型列表',
  'Sync now': '立即同步',
  'Model ID': '模型 ID',
  'Context': '上下文',
  'Output': '输出',
  'Tools': '工具',
  'Reason': '推理',
  'Attach': '附件',
  'Endpoint': '端点',
  '(seed)': '（种子）',
  ' free models total (auto-synced every 10 minutes from public registry)': ' 个免费模型（每 10 分钟自动从公共注册表同步）',
  'OpenCode stats': 'OpenCode 统计',
  'Requests': '请求数',
  'Input tokens': '输入 token',
  'Output tokens': '输出 token',
  'Compaction': '压缩',
  'Rate-limited': '限流次数',
  'Today': '今日',
  'Total': '累计',
  'Per-model breakdown (today)': '按模型拆分（今日）',
  'No data': '暂无数据',
  'Failover active (opencode unavailable, requests go to the cline pool)': '故障转移生效中（opencode 不可用，请求转到 cline 池）',
  'Normal': '正常',
  // 模型状态标签
  'available': '可用',
  'empty response': '空响应',
  'subscription': '订阅制',
  'removed': '已下架',
  'error': '错误',
  'unprobed': '未探测',
  'free': '免费',
  'uses quota': '消耗配额',
  // 通用 toast / 确认 / 片段
  'Saved ': '已保存 ',
  ' headers': ' 个请求头',
  ' proxies': ' 个代理',
  ' proxies configured': ' 个代理已配置',
  'routing through the pool': '经代理池路由',
  'direct (pool list is empty)': '直连（代理列表为空）',
  'upstream disabled': '上游已禁用',
  'Proxy pool saved': '代理池已保存',
  'Cline proxy setting saved': 'Cline 代理设置已保存',
  'Proxy isolation setting saved': '代理隔离设置已保存',
  'Proxy binding saved': '代理绑定已保存',
  'Save failed: ': '保存失败：',
  'Assign failed: ': '分配失败：',
  'Key proxy binding saved': 'Key 代理绑定已保存',
  'Test failed: ': '测试失败：',
  'Check failed: ': '检查失败：',
  'Delete failed: ': '删除失败：',
  'Refresh failed: ': '刷新失败：',
  'Export failed: ': '导出失败：',
  'Update failed: ': '更新失败：',
  'Generation failed: ': '生成失败：',
  'Add failed: ': '添加失败：',
  'Import failed: ': '导入失败：',
  'Create failed: ': '创建失败：',
  'Sync failed: ': '同步失败：',
  'Mint failed: ': '铸造失败：',
  'Failed to load accounts: ': '账号加载失败：',
  'Failed to load: ': '加载失败：',
  'Account deleted': '账号已删除',
  'All accounts deleted': '已删除全部账号',
  'All tokens refreshed': '全部 token 已刷新',
  'All keys deleted': '已删除全部 key',
  'Key deleted': 'key 已删除',
  'Key generated': 'key 已生成',
  'New key generated (click to copy)': '新 key 已生成（点击复制）',
  'Copied to clipboard': '已复制到剪贴板',
  'Accounts exported (JSON)': '账号已导出（JSON）',
  'Account added: ': '账号已添加：',
  'Account added!': '账号添加成功！',
  'Account ': '账号 ',
  ' — ': ' —— ',
  '(was: ': '（原状态：',
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
  'Strategy updated to: ': '调度策略已更新为：',
  'Rows with a value but no key were ignored': '有值但没有键名的行已被忽略',
  'Headers saved': '请求头已保存',
  'Enter a token or API key': '请输入 token 或 API key',
  'Enter account data': '请输入账号数据',
  'Import complete': '导入完成',
  'Imported ': '已导入 ',
  ' accounts': ' 个账号',
  'Enter an alias ID': '请输入别名 ID',
  'Select a target model': '请选择目标模型',
  'Combo created: ': '自定义别名已创建：',
  'Combo deleted': '自定义别名已删除',
  'Delete combo ': '删除自定义别名 ',
  '? Clients using this alias will no longer be able to call it.': '？使用该别名的客户端将无法继续调用。',
  'Delete this account?': '确定删除该账号？',
  'Delete ALL accounts? This cannot be undone!': '确定删除全部账号？此操作不可撤销！',
  'Delete this key?': '确定删除该 key？',
  'Delete ALL API keys?': '确定删除全部 API key？',
  'Assign the proxy pool evenly to ALL accounts (main = pool[i%N], backup = pool[(i+1)%N])? Existing bindings are overwritten.': '把代理池均匀分配给全部账号（主 = 池[i%N]，辅 = 池[(i+1)%N]）？现有绑定会被覆盖。',
  'Proxies assigned': '代理已分配',
  'Connected via ': '经出口 ', // (placeholder, unused fragment guard)
  'OAuth failed': 'OAuth 登录失败',
  'OAuth failed: ': 'OAuth 登录失败：',
  'OAuth polling stopped': 'OAuth 轮询已停止',
  'Failed: ': '失败：',
  'Polling failed: ': '轮询失败：',
  'Error: ': '错误：',
  'Starting...': '启动中...',
  'Testing': '测试中',
  'Checking': '检查中',
  'Force minting all sessions…': '正在强制重铸全部会话…',
  'Minting missing sessions…': '正在铸造缺失的会话…',
  'minting ': '铸造中 ',
  'last mint ': '上次铸造 ',
  ' (force)': '（强制）',
  'Running — ': '进行中 —— ',
  ' minted, ': ' 成功, ',
  ' failed, ': ' 失败, ',
  ' pending': ' 待处理',
  'Done — ': '完成 —— ',
  ' already live': ' 已是 live',
  ' skipped (already live)': ' 跳过（已是 live）',
  'Recovers ': '恢复于 ',
  'No opencode keys configured': '没有配置任何 opencode key',
  'Failed to load': '加载失败',
  'Connecting to WorkOS...': '正在连接 WorkOS...',
  'Open the link in your browser and enter the code': '在浏览器中打开链接并输入代码',
  'Default model saved: ': '默认模型已保存：',
  'Invalid proxy format: ': '代理格式无效：',
  'No models': '没有模型',
  '· official feed: ': '· 官方源同步于 ',
  ' Sync complete': '同步完成',
  'Sync complete': '同步完成',
  'Check complete': '检查完成',
  'cooldown': '冷却中',
  'active': '可用',
  'expired': '已失效'
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
    if (t) el.setAttribute(a, t);
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
  _('quickstartApi').textContent = 'http://' + window.location.host + '/v1';
}

function toast(msg, t, duration) {
  const el = _('toast');
  el.textContent = msg;
  el.style.whiteSpace = 'pre-line';
  el.className = 'toast ' + (t || 'info') + ' show';
  clearTimeout(el._timer);
  el._timer = setTimeout(() => el.classList.remove('show'), duration || 3500);
}

// ========== Navigation ==========
document.querySelectorAll('.nav-item').forEach(el => {
  el.addEventListener('click', () => {
    if (el.classList.contains('active')) return;
    document.querySelectorAll('.nav-item').forEach(e => e.classList.remove('active'));
    el.classList.add('active');
    document.querySelectorAll('.tab-panel').forEach(e => e.style.display = 'none');
    _('tab-' + el.dataset.tab).style.display = 'block';
    if (el.dataset.tab === 'dashboard') { loadStats(); loadAccounts(); }
    if (el.dataset.tab === 'accounts') loadAccounts();
    if (el.dataset.tab === 'settings') { loadKeys(); loadModels(); loadConfig(); }
    if (el.dataset.tab === 'logs') loadLogs();
    if (el.dataset.tab === 'proxypool') loadProxyPool();
    if (el.dataset.tab === 'opencode') { loadOcConfig(); loadOcModels(); loadOcStats(); loadOcSessions(); }
    if (el.dataset.tab === 'combos') { loadCombos(); fillComboModels(); }
  });
});

function switchTab(name) {
  document.querySelectorAll('.nav-item').forEach(e => {
    e.classList.toggle('active', e.dataset.tab === name);
  });
  document.querySelectorAll('.tab-panel').forEach(e => e.style.display = 'none');
  _('tab-' + name).style.display = 'block';
  if (name === 'dashboard') { loadStats(); loadAccounts(); }
  if (name === 'accounts') loadAccounts();
  if (name === 'settings') { loadKeys(); loadModels(); }
  if (name === 'logs') loadLogs();
  if (name === 'proxypool') loadProxyPool();
  if (name === 'opencode') { loadOcConfig(); loadOcModels(); loadOcStats(); loadOcSessions(); }
  if (name === 'combos') { loadCombos(); fillComboModels(); }
}

// Import sub-tabs
document.querySelectorAll('#importTabs .tab').forEach(el => {
  el.addEventListener('click', () => {
    document.querySelectorAll('#importTabs .tab').forEach(e => e.classList.remove('active'));
    el.classList.add('active');
    document.querySelectorAll('#import-oauth,#import-token,#import-batch').forEach(e => e.classList.remove('active'));
    _('import-' + el.dataset.tab).classList.add('active');
  });
});

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
async function loadStats() {
  try {
    const d = await api('GET', '/stats');
    const s = d.data;
    _('statTotal').textContent = s.total;
    _('statActive').textContent = s.active;
    _('statCooldown').textContent = s.cooldown;
    _('statExpired').textContent = s.expired;
    if (s.version) _('settingVersion').value = s.version;
    // 策略下拉由 /config 加载填写（loadConfig），统计刷新不回写表单，
    // 避免把用户正在编辑的选项覆盖掉
  } catch (e) { /* ignore */ }
}

// ========== Accounts ==========
async function loadAccounts() {
  try {
    await loadProxyListCache();
    const d = await api('GET', '/accounts');
    const list = d.data.accounts;
    const tbody = _('accountTableBody');
    if (!list || list.length === 0) {
      tbody.innerHTML = '<tr><td colspan="7" class="empty">No accounts yet — add one on the <a href="#" onclick="switchTab(\'import\')" style="color:var(--accent);cursor:pointer">Add accounts</a> page</td></tr>';
      return;
    }
    const sn = { active: 'Active', cooldown: 'Cooldown', expired: 'Expired' };
    tbody.innerHTML = list.map(a => {
      const lu = a.lastUsed ? new Date(a.lastUsed).toLocaleString('en-US') : '-';
      const cr = a.createdAt ? new Date(a.createdAt).toLocaleString('en-US') : '-';
      // Cooldown label: show estimated recovery time
      let statusExtra = '';
      if (a.status === 'cooldown') {
        const until = a.cooldownUntil ? new Date(a.cooldownUntil).toLocaleString('en-US') : '';
        statusExtra = until ? '<div style="font-size:10px;color:var(--text3);margin-top:2px">' + T('Recovers ') + esc(until) + '</div>' : '';
      }
      // Proxy binding cells: main/backup comboboxes (Global / Direct / proxies
      // with type-ahead) saved on change. A binding that no longer exists in
      // the pool gets a stale marker — isolation keeps skipping the account
      // until it is fixed. The direct sentinel is never stale.
      // omitempty 序列化下未绑定账号没有 proxyMain/proxyBackup 键，归一化为 ''。
      const pMain = a.proxyMain || '';
      const pBackup = a.proxyBackup || '';
      const isStale = v => v && v !== EGRESS_DIRECT && !proxyListCache.includes(v);
      const stale = isStale(pMain) || isStale(pBackup);
      // 主 = 全局/直连 时辅跟随锁定（渲染期同样生效）
      const backupLocked = pMain === '' || pMain === EGRESS_DIRECT;
      const backupVal = backupLocked ? pMain : pBackup;
      const bindCell = '<td style="white-space:nowrap">' +
        egComboHTML('acc', a.accountId, 'main', pMain, T('Main exit — used whenever available')) + ' ' +
        egComboHTML('acc', a.accountId, 'backup', backupVal, T('Backup exit — used when main is cooling/removed'), backupLocked) +
        (stale ? ' <span title="' + esc(T('Bound proxy is no longer in the proxy pool — the account is skipped until fixed')) + '" style="color:var(--danger)">⚠</span>' : '') +
        '</td>';
      const st = esc(a.status);
      return '<tr>' +
        '<td>' + esc(a.email) + '</td>' +
        '<td><span class="status ' + st + '"><span class="status-dot ' + st + '"></span>' + (sn[a.status] || st) + '</span>' + statusExtra + '</td>' +
          '<td title="Today ' + fmtNum(a.tokensToday) + ' / total ' + fmtNum(a.tokensTotal) + ' tokens (exact when upstream returns usage, otherwise estimated)">' + fmtTokens(a.tokensToday) + ' / ' + fmtTokens(a.tokensTotal) + '</td>' +
        '<td class="mono" style="font-size:11px">' + lu + '</td>' +
        '<td class="mono" style="font-size:11px">' + cr + '</td>' +
        bindCell +
        '<td style="white-space:nowrap">' +
          // Action buttons dispatch via data-* attributes + delegated listeners:
          // inline onclick string concatenation gets HTML-decoded before JS parsing,
          // so even escaped quotes could be bypassed
          '<button class="btn btn-sm" data-act="test" data-acc="' + esc(a.accountId) + '" title="Test whether the account works (success clears cooldown/expired state)">Test</button> ' +
          '<button class="btn btn-sm" data-act="reset" data-acc="' + esc(a.accountId) + '" title="Probe the rate limit and lift it: probes upstream; if still limited, stays in cooldown and shows recovery time">Reset</button> ' +
          '<button class="btn btn-sm btn-danger" data-act="delete" data-acc="' + esc(a.accountId) + '" title="Delete">Delete</button>' +
        '</td></tr>';
    }).join('');
    tbody.onchange = null;
    tbody.onclick = e => {
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
async function loadProxyListCache() {
  try {
    const d = await api('GET', '/opencode/config');
    proxyListCache = (d.data.proxies || []).slice();
  } catch (e) { /* keep previous cache */ }
}
const maskProxyLabel = p => String(p || '').replace(/\/\/[^@/]*@/, '//***@');
async function saveAccountProxy(id, input) {
  const row = input.closest('tr');
  const main = row.querySelector('.eg-input[data-slot="main"]').dataset.value;
  const backup = row.querySelector('.eg-input[data-slot="backup"]').dataset.value;
  try {
    await api('POST', '/accounts/proxy', { accountId: id, main: main, backup: backup });
    toast(T('Proxy binding saved'), 'success');
  } catch (e) {
    toast('Save failed: ' + e.message, 'error');
    loadAccounts();
  }
}
async function assignProxiesEvenly() {
  if (!confirm(T('Assign the proxy pool evenly to ALL accounts (main = pool[i%N], backup = pool[(i+1)%N])? Existing bindings are overwritten.'))) return;
  try {
    const d = await api('POST', '/accounts/proxy/assign', {});
    toast(d.message || T('Proxies assigned'), 'success');
    loadAccounts();
  } catch (e) { toast('Assign failed: ' + e.message, 'error'); }
}

async function clearAccountProxies() {
  if (!confirm(T('Clear proxy bindings on ALL accounts? Every account returns to the Global default.'))) return;
  try {
    const d = await api('POST', '/accounts/proxy/clear', {});
    toast(d.message || T('Bindings cleared'), 'success');
    loadAccounts();
  } catch (e) { toast('Clear failed: ' + e.message, 'error'); }
}

async function clearZenKeyProxies() {
  if (!confirm(T('Clear proxy bindings on ALL keys? Every key returns to the Global default (enabled state is kept).'))) return;
  try {
    const d = await api('POST', '/opencode/keys/proxy/clear', {});
    toast(d.message || T('Bindings cleared'), 'success');
    loadOcConfig();
  } catch (e) { toast('Clear failed: ' + e.message, 'error'); }
}

async function enableAllZenKeys(enabled) {
  if (!confirm(enabled ? T('Enable auto-minting on ALL keys?') : T('Disable auto-minting on ALL keys?'))) return;
  try {
    const d = await api('POST', '/opencode/keys/enabled/all', { enabled: enabled });
    toast(d.message || T('Enabled state saved'), 'success');
    loadOcConfig();
  } catch (e) { toast('Save failed: ' + e.message, 'error'); }
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
function egComboHTML(kind, id, slot, value, title, disabled) {
  const t = disabled ? T('Follows the main slot') : title;
  return '<input type="text" class="eg-input" data-eg="' + kind + '" data-egid="' + esc(id) + '" data-slot="' + slot + '" data-value="' + esc(value || '') + '" value="' + esc(egLabel(value)) + '" title="' + esc(t) + '"' + (disabled ? ' disabled' : '') + ' autocomplete="off" spellcheck="false">';
}
// 主槽位联动：主 = 全局/直连 时辅自动跟随相同值并只读；主 = 代理时辅解锁。
// 被锁定期间辅的旧值会被跟随值覆盖（解锁后辅回到"全局"，可重新选择）。
function egApplyMainLinkage(row, mainValue) {
  const backup = row ? row.querySelector('.eg-input[data-slot="backup"]') : null;
  if (!backup) return;
  if (mainValue === '' || mainValue === EGRESS_DIRECT) {
    backup.dataset.value = mainValue;
    backup.value = egLabel(mainValue);
    backup.disabled = true;
    backup.title = T('Follows the main slot');
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
  const items = [
    { v: '', label: T('Global'), active: cur === '' },
    { v: EGRESS_DIRECT, label: T('Direct'), active: cur === EGRESS_DIRECT }
  ];
  // 联想阈值：输入含中文时中文字符 ≥2 触发，否则长度 ≥3；未达标不展示代理
  const cjk = [...q].filter(c => c.codePointAt(0) > 0x7f).length;
  const ready = q === '' || (cjk > 0 ? cjk >= 2 : q.length >= 3);
  const ql = q.toLowerCase();
  const matches = p => {
    const label = egLabel(p).toLowerCase();
    return label.includes(ql) || p.toLowerCase().includes(ql);
  };
  const proxies = q === ''
    ? proxyListCache.map(p => ({ v: p, label: egLabel(p), active: cur === p }))
    : (ready
        ? proxyListCache.filter(matches).map(p => ({ v: p, label: egLabel(p), active: cur === p }))
        : []);
  let html = items.concat(proxies).map(it =>
    '<div class="eg-item' + (it.active ? ' active' : '') + '" data-value="' + esc(it.v) + '">' + esc(it.label) + '</div>'
  ).join('');
  if (q !== '' && !proxies.length) html += '<div class="eg-item eg-hint">' + T('No matching proxies') + '</div>';
  menu.innerHTML = html;
}
function egCommit(input, value) {
  input.dataset.value = value;
  input.value = egLabel(value);
  egCloseMenu();
  input.blur();
  if (input.dataset.slot === 'main') egApplyMainLinkage(input.closest('tr'), value);
  if (input.dataset.eg === 'acc') saveAccountProxy(input.dataset.egid, input);
  else if (input.dataset.eg === 'key') saveZenKeyProxy(parseInt(input.dataset.egid, 10), input);
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
let importPayload = null;

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

function onImportFileChange(event) {
  const file = event.target.files[0];
  if (!file) return;
  const reader = new FileReader();
  reader.onload = () => {
    try {
      const parsed = JSON.parse(reader.result);
      if (parsed.version !== 1) throw new Error(T('Unsupported backup version'));
      importPayload = JSON.stringify(parsed);
      _('importBtn').disabled = false;
      _('importResult').innerHTML = '<div style="color:var(--accent2);font-size:12px">' + esc(file.name) + ' — ' + esc(T('ready to import')) + '</div>';
    } catch (e) {
      importPayload = null;
      _('importBtn').disabled = true;
      toast(T('Invalid backup file: ') + e.message, 'error');
    }
  };
  reader.readAsText(file);
  event.target.value = '';
}

async function importConfigFromFile() {
  if (!importPayload) { toast(T('Choose a backup JSON file first'), 'error'); return; }
  if (!confirm(T('Import replaces sections present in the file (accounts / keys / proxies / combos ...). Continue?'))) return;
  try {
    const d = await api('POST', '/config/import', JSON.parse(importPayload));
    const imp = (d.data && d.data.imported) || {};
    const parts = Object.keys(imp).map(k => k + '=' + imp[k]);
    toast(T('Config imported') + ' — ' + parts.join(', '), 'success');
    setTimeout(() => location.reload(), 1200);
  } catch (e) { toast(T('Import failed: ') + e.message, 'error'); }
}

async function deleteAllAccounts() {
  if (!confirm(T('Delete ALL accounts? This cannot be undone!'))) return;
  try {
    await api('POST', '/accounts/delete-all', {});
    toast('All accounts deleted', 'success');
    loadAccounts(); loadStats();
  } catch (e) { toast(T('Delete failed: ') + e.message, 'error'); }
}

async function refreshAllTokens() {
  try {
    await api('POST', '/accounts/refresh-all', {});
    toast('All tokens refreshed', 'success');
    loadAccounts(); loadStats();
  } catch (e) { toast(T('Refresh failed: ') + e.message, 'error'); }
}

// ========== OAuth login ==========
async function startOAuth() {
  const btn = _('oauthBtn');
  btn.disabled = true;
  btn.innerHTML = '<span class="loading"></span> Starting...';
  _('oauthProgress').style.display = 'block';
  _('oauthResult').style.display = 'none';
  _('oauthStatus').textContent = T('Connecting to WorkOS...');
  try {
    const d = await api('POST', '/oauth/start');
    const s = d.data;
    _('oauthStatus').textContent = T('Open the link in your browser and enter the code');
    const u = _('oauthUrl');
    const vuri = String(s.verificationUri || '');
    u.textContent = vuri;
    // 仅对 https 链接设置 href：非 https 的 verificationUri 只展示不可点，
    // 防止 javascript: 等异常 scheme 成为注入面
    if (/^https:\/\//i.test(vuri)) { u.href = vuri; } else { u.removeAttribute('href'); }
    _('oauthUserCode').textContent = s.userCode;
    let pollFails = 0;
    const poll = setInterval(async () => {
      try {
        const r = await api('GET', '/oauth/status?sessionId=' + s.sessionId);
        pollFails = 0;
        if (r.data && r.data.done) {
          clearInterval(poll);
          btn.disabled = false;
          btn.innerHTML = 'Start OAuth login';
          if (r.data.success) {
            _('oauthProgress').style.display = 'none';
            _('oauthResult').innerHTML = '<div style="color:var(--accent2);font-weight:600;font-size:14px">' + T('Account added: ') + esc(r.data.email) + '</div>';
            _('oauthResult').style.display = 'block';
            loadAccounts(); loadStats();
            toast('Account added!', 'success');
          } else {
            _('oauthStatus').textContent = T('Failed: ') + (r.data.error || 'unknown error');
            toast('OAuth failed', 'error');
          }
        }
      } catch(e) {
        // 连续失败(会话丢失/服务重启)后停止轮询并恢复按钮，避免永久卡死；
        // 偶发网络抖动在 5 次(约10s)内自愈
        if (++pollFails >= 5) {
          clearInterval(poll);
          btn.disabled = false;
          btn.innerHTML = 'Start OAuth login';
          _('oauthStatus').textContent = T('Polling failed: ') + (e && e.message ? e.message : 'session not found');
          toast('OAuth polling stopped', 'error');
        }
      }
    }, 2000);
  } catch (e) {
    btn.disabled = false;
    btn.innerHTML = 'Start OAuth login';
    _('oauthStatus').textContent = T('Error: ') + e.message;
    toast(T('OAuth failed: ') + e.message, 'error');
  }
}

// ========== Token import ==========
// Credential type auto-detection: "sk_..." = static Cline API key, anything
// else = OAuth refreshToken (the backend validates refresh tokens upstream;
// API keys are pooled as-is).
const credPayload = t => t.startsWith('sk_') ? { apiToken: t } : { refreshToken: t };

async function addByToken() {
  const token = _('tokenInput').value.trim();
  if (!token) { toast('Enter a token or API key', 'error'); return; }
  const email = _('tokenEmail').value.trim();
  try {
    const d = await api('POST', '/accounts/add', { ...credPayload(token), email: email || undefined });
    toast(T('Account added: ') + (d.data.email || ''), 'success');
    _('tokenInput').value = '';
    _('tokenEmail').value = '';
    loadAccounts(); loadStats();
  } catch (e) { toast(T('Add failed: ') + e.message, 'error'); }
}

// ========== Batch import ==========
const parseBatchInput = raw => {
  try {
    let tokens = JSON.parse(raw);
    if (!Array.isArray(tokens)) tokens = [tokens];
    return tokens;
  } catch {
    return raw.split('\n').map(t => t.trim()).filter(Boolean).map(credPayload);
  }
};

async function batchImport() {
  const raw = _('batchInput').value.trim();
  if (!raw) { toast('Enter account data', 'error'); return; }
  const tokens = parseBatchInput(raw);
  try {
    const d = await api('POST', '/batch-import', { tokens });
    toast(d.message || 'Import complete', 'success');
    _('batchInput').value = '';
    loadAccounts(); loadStats();
  } catch (e) { toast(T('Import failed: ') + e.message, 'error'); }
}

async function handleFileImport(event) {
  const file = event.target.files[0];
  if (!file) return;
  const text = await file.text();
  const tokens = parseBatchInput(text);
  try {
    const d = await api('POST', '/batch-import', { tokens });
    toast(d.message || T('Imported ') + tokens.length + T(' accounts'), 'success');
    loadAccounts(); loadStats();
  } catch (e) { toast(T('Import failed: ') + e.message, 'error'); }
  event.target.value = '';
}

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

async function deleteAllKeys() {
  if (!confirm(T('Delete ALL API keys?'))) return;
  try {
    const d = await api('GET', '/keys');
    const keys = d.data.keys || [];
    for (const k of keys) await api('POST', '/keys/delete', { key: k });
    toast('All keys deleted', 'success');
    loadKeys();
  } catch (e) { toast(T('Delete failed: ') + e.message, 'error'); }
}

function copyText(t) {
  navigator.clipboard.writeText(t).then(() => toast('Copied to clipboard', 'success')).catch(() => {
    const ta = document.createElement('textarea');
    ta.value = t; document.body.appendChild(ta); ta.select(); document.execCommand('copy'); document.body.removeChild(ta);
    toast('Copied to clipboard', 'success');
  });
}

// ========== Request logs ==========
const ROUTE_LABEL = { zen: 'opencode', cline: 'cline pool', admin: 'admin', meta: 'meta', other: 'other' };
const STATUS_CLASS = s => s >= 500 ? 'color:var(--danger)' : (s >= 400 ? 'color:var(--amber)' : 'color:var(--accent2)');
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
    if (!logs.length) { tbody.innerHTML = '<tr><td colspan="9" class="empty">No requests logged</td></tr>'; return; }
    tbody.innerHTML = logs.map(l => {
      const t = l.time ? new Date(l.time).toLocaleString('en-US') : '-';
      const route = ROUTE_LABEL[l.route] || l.route || '-';
      const st = l.status || 0;
      return '<tr>' +
        '<td class="mono" style="font-size:11px">' + t + '</td>' +
        '<td class="mono" style="font-size:11px">' + esc(l.client || '-') + '</td>' +
        '<td>' + esc(l.method || '-') + '</td>' +
        '<td class="mono" style="font-size:11px">' + esc(l.path || '-') + '</td>' +
        '<td class="mono" style="font-size:12px">' + esc(l.model || '-') + '</td>' +
        '<td><span class="model-tag">' + esc(route) + '</span></td>' +
        '<td class="mono" style="font-size:11px">' + esc(l.upstream || '-') + '</td>' +
        '<td style="font-weight:600;color:' + STATUS_CLASS(st) + '">' + st + '</td>' +
        '<td class="mono" style="font-size:11px">' + (l.duration_ms != null ? l.duration_ms + ' ms' : '-') + '</td>' +
      '</tr>';
    }).join('');
  } catch (e) { tbody.innerHTML = '<tr><td colspan="9" class="empty">Failed to load</td></tr>'; }
}

// ========== Export accounts ==========
async function exportAccounts() {
  try {
    const res = await fetch(API + '/accounts/export');
    if (!res.ok) throw new Error('HTTP ' + res.status);
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url; a.download = 'cline-accounts-export.json';
    document.body.appendChild(a); a.click(); document.body.removeChild(a);
    URL.revokeObjectURL(url);
    toast('Accounts exported (JSON)', 'success');
  } catch (e) { toast(T('Export failed: ') + e.message, 'error'); }
}

// ========== Config updates ==========
async function updateConfig() {
  const strategy = _('settingStrategy').value;
  try {
    await api('POST', '/config/update', { strategy });
    toast(T('Strategy updated to: ') + strategy, 'success');
  } catch (e) { toast(T('Update failed: ') + e.message, 'error'); }
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
    _('headerSaveResult').innerHTML =
      '<div style="color:var(--accent2);font-size:12px">' + T('Saved ') + Object.keys(d.data.headers).length + T(' headers') + '</div>';
    setTimeout(() => _('headerSaveResult').innerHTML = '', 5000);
    loadConfig();
  } catch (e) { toast(T('Save failed: ') + e.message, 'error'); }
}

const MODEL_STYLE = {
  active:  { label: 'available', css: 'color:var(--accent2);border:1px solid rgba(52,211,153,.5);background:rgba(52,211,153,.08)' },
  empty:   { label: 'empty response', css: 'color:var(--amber);border:1px solid rgba(245,158,11,.5);background:rgba(245,158,11,.08)' },
  pass:    { label: 'subscription', css: 'color:var(--amber);border:1px solid rgba(245,158,11,.5);background:rgba(245,158,11,.08)' },
  removed: { label: 'removed', css: 'color:var(--text3);border:1px solid var(--border)' },
  error:   { label: 'error', css: 'color:var(--danger);border:1px solid rgba(248,113,113,.5);background:rgba(248,113,113,.08)' },
  unknown: { label: 'unprobed', css: 'color:var(--text3);border:1px dashed var(--border-strong)' }
};
const COST_LABEL = { free: 'free', pass: 'subscription', quota: 'uses quota' };

async function loadModels() {
  try {
    const d = await api('GET', '/models');
    const models = d.data.models || [];
    let info = '';
    if (d.data.lastSync) info += T('· official feed: ') + new Date(d.data.lastSync).toLocaleTimeString('en-US');
    _('modelsProbeInfo').textContent = info;
    // OpenCode 免费模型（网关设置页分两块展示：Cline 在上、OpenCode 在下）
    const zen = await api('GET', '/opencode/models').then(r => r.data.models || []).catch(() => []);
    const row = (inner) => '<div style="display:flex;align-items:center;gap:10px;padding:8px 12px;margin:5px 0;background:rgba(148,163,184,.06);border:1px solid var(--border);border-radius:10px;transition:.15s">' + inner + '</div>';
    const clineBlock = models.length
      ? models.map(m => {
          const st = MODEL_STYLE[m.status] || MODEL_STYLE.unknown;
          const cost = COST_LABEL[m.cost] || m.cost || '';
          const synced = m.syncedAt ? new Date(m.syncedAt).toLocaleTimeString('en-US') : '-';
          return row(
            '<span style="font-family:\'JetBrains Mono\',monospace;font-size:13px;flex:1">' + esc(m.id) + '</span>' +
            (m.cost === 'free' ? '<span style="font-size:11px;color:var(--accent2)">' + T('no charge') + '</span>' : '') +
            (cost ? '<span class="model-tag">' + esc(cost) + '</span>' : '') +
            '<span class="model-tag" style="' + st.css + '">' + st.label + '</span>' +
            '<span style="font-size:11px;color:var(--text3);min-width:60px;text-align:right">' + synced + '</span>');
        }).join('')
      : '<div class="empty">' + T('No models') + '</div>';
    const zenBlock = zen.length
      ? zen.map(m => row(
            '<span style="font-family:\'JetBrains Mono\',monospace;font-size:13px;flex:1">' + esc(m.id) + '</span>' +
            '<span class="model-tag" title="' + esc(T('Context')) + '">' + esc(m.context || '-') + '</span>' +
            '<span class="model-tag" title="' + esc(T('Output')) + '">' + esc(m.output || '-') + '</span>' +
            '<span class="model-tag">' + esc(m.source === 'live' ? T('live') : T('(seed)')) + '</span>')).join('')
      : '<div class="empty">' + T('No models') + '</div>';
    _('modelsList').innerHTML =
      '<div style="font-size:12px;font-weight:600;margin:2px 0 8px">' + T('Cline free models') + '</div>' + clineBlock +
      '<div style="font-size:12px;font-weight:600;margin:14px 0 8px">' + T('OpenCode free models') + '</div>' + zenBlock;
  } catch (e) { _('modelsList').textContent = T('Failed to load'); }
}

async function refreshModels() {
  try {
    _('modelsProbeInfo').textContent = '· syncing...';
    // 两个上游的模型源一起刷新（Cline feed + OpenCode 目录）
    const [d, z] = await Promise.all([
      api('POST', '/models/refresh').catch(e => ({ message: e.message })),
      api('POST', '/opencode/models/refresh').catch(e => ({ message: e.message }))
    ]);
    const msg = [d.message || (d.data && d.data.message), z.message || (z.data && z.data.message)].filter(Boolean).join(' | ');
    toast(msg || 'Sync started', 'info');
    setTimeout(loadModels, 3000);
  } catch (e) { toast(T('Refresh failed: ') + e.message, 'error'); _('modelsProbeInfo').textContent = ''; }
}

async function loadModelOptions() {
  try {
    // 默认模型下拉：合并 Cline 与 OpenCode 两个上游的全部免费模型
    const [d, z] = await Promise.all([
      api('GET', '/models'),
      api('GET', '/opencode/models').catch(() => ({ data: { models: [] } }))
    ]);
    const cline = d.data.models || [];
    const zen = z.data.models || [];
    const sel = _('settingDefModel');
    if (!sel) return;
    sel.innerHTML = cline.map(m => {
      const st = MODEL_STYLE[m.status] || MODEL_STYLE.unknown;
      return '<option value="' + esc(m.id) + '">' + esc(m.id) + ' (' + st.label + ')</option>';
    }).join('') + zen.map(m => '<option value="' + esc(m.id) + '">' + esc(m.id) + ' (OpenCode)</option>').join('');
    const c = await api('GET', '/config');
    if (c.data.defaultModel) sel.value = c.data.defaultModel;
    if (!sel.value && cline.length) sel.value = cline[0].id;
  } catch (e) { /* ignore */ }
}

async function saveDefaultModel() {
  const v = _('settingDefModel').value;
  if (!v) { toast('Select a model', 'error'); return; }
  try {
    const d = await api('POST', '/config/update', { defaultModel: v });
    toast(T('Default model saved: ') + d.data.defaultModel, 'success');
  } catch (e) { toast(T('Save failed: ') + e.message, 'error'); }
}

// ========== Config load ==========
async function loadConfig() {
  try {
    const d = await api('GET', '/config');
    const c = d.data;
    if (c.address) _('settingAddr').value = c.address;
    if (c.strategy) _('settingStrategy').value = c.strategy;
    if (c.version) _('settingVersion').value = c.version;
    if (c.poolPath && _('settingPoolPath')) _('settingPoolPath').value = c.poolPath;
    loadModelOptions();
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
    _('ocKeys').value = (c.keys && c.keys.length ? c.keys : [c.key || 'public']).join('\n');
    const ks = c.keyStates || [];
    renderOcKeyStates(ks);
    _('ocBaseURL').value = c.baseURL || '';
    _('ocMaxConc').value = c.maxConcurrency || 8;
    _('ocRetries').value = c.retries || 3;
    _('ocFailover').value = String(c.failover);
    _('ocFailoverCount').value = c.failoverCount || 3;
    _('ocFailoverMinutes').value = c.failoverMinutes || 5;
    _('ocCompactAuto').value = String(c.compaction ? c.compaction.auto : true);
    _('ocCompactBuffer').value = c.compaction ? c.compaction.buffer : 20000;
    _('ocKeepTokens').value = c.compaction ? c.compaction.keepTokens : 8000;
    _('ocSummaryModel').value = c.compaction ? (c.compaction.summaryModel || '') : '';
    _('ocMaxSummary').value = c.compaction ? c.compaction.maxSummary : 4096;
    const rt = c.runtime || {};
    _('ocFailoverInfo').innerHTML = rt.failoverActive
      ? '<span style="color:var(--danger)">Failover active (opencode unavailable, requests go to the cline pool)</span>'
      : '<span style="color:var(--accent2)">Normal</span>';
  } catch (e) { /* ignore */ }
}

async function saveOcConfig() {
  const keys = _('ocKeys').value.split('\n').map(s => s.trim()).filter(Boolean);
  if (!keys.length) { toast('API keys must not be empty (use "public" if you have no key)', 'error'); return; }
  const body = {
    keys: keys,
    baseURL: _('ocBaseURL').value.trim(),
    proxies: ocCfgCache.proxies || [],
    proxyStrategy: ocCfgCache.proxyStrategy || 'round_robin',
    maxConcurrency: parseInt(_('ocMaxConc').value) || 8,
    retries: parseInt(_('ocRetries').value) || 3,
    failover: _('ocFailover').value === 'true',
    failoverCount: parseInt(_('ocFailoverCount').value) || 3,
    failoverMinutes: parseInt(_('ocFailoverMinutes').value) || 5,
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

// per-key 状态表：key 掩码 / 用量 / 会话 / 冷却（含预计恢复时刻）/ Test 按钮。
// Test 与 cline 账号的同语义：真实探测，成功即复位该 key 的冷却。
// Proxy binding 列：主/辅出口下拉，修改即保存（隔离模式语义见 Proxy pool 页）。
function renderOcKeyStates(ks) {
  const tb = _('ocKeysBody');
  if (!tb) return;
  tb.innerHTML = ks.length
    ? ks.map(k => {
        const st = k.sessionLive
          ? '<span style="color:var(--accent2)">live</span>'
          : (k.sessionMinted ? '<span style="color:var(--danger)">stale</span>' : '<span style="color:var(--danger)">not minted</span>');
        const cool = k.cooling
          ? '<span style="color:var(--danger)">' + T('cooling') + (k.cooldownUntil ? T(' · until ') + esc(fmtWhen(k.cooldownUntil)) : '') + '</span>'
          : '<span style="color:var(--text2)">-</span>';
        // omitempty 下未绑定 key 没有这两个键，归一化为 ''
        const pMain = k.proxyMain || '';
        const pBackup = k.proxyBackup || '';
        const backupLocked = pMain === '' || pMain === EGRESS_DIRECT;
        const backupVal = backupLocked ? pMain : pBackup;
        const bind = '<td style="white-space:nowrap">' +
          egComboHTML('key', k.index, 'main', pMain, T('Main exit — used whenever available')) + ' ' +
          egComboHTML('key', k.index, 'backup', backupVal, T('Backup exit — used when main is cooling/removed'), backupLocked) +
          (k.proxyStale ? ' <span title="' + esc(T('Bound proxy is no longer in the proxy pool — the key is skipped until fixed')) + '" style="color:var(--danger)">⚠</span>' : '') +
          '</td>';
        // 是否启用：勾选后该 key 才参与自动铸造（启动补缺/403/周期/手动 Mint）。
        // 新添加的 key 默认未勾选。
        const en = '<td style="text-align:center">' + (k.keyMask === 'public (no key)'
          ? '<span style="color:var(--text3)">-</span>'
          : '<input type="checkbox" data-ze="' + k.index + '"' + (k.enabled ? ' checked' : '') + ' title="' + esc(T('Participates in auto-minting')) + '">') + '</td>';
        return '<tr><td>#' + (k.index + 1) + (k.current ? ' <span style="color:var(--accent)" title="next in rotation">●</span>' : '') + '</td>' +
          '<td style="font-family:monospace;font-size:11px">' + esc(k.keyMask) + '</td>' +
          '<td>' + (k.usage || 0) + '</td>' +
          '<td>' + st + '</td>' +
          '<td>' + cool + '</td>' +
          bind +
          '<td>' + (k.keyMask === 'public (no key)' ? '' : '<button class="btn btn-sm" data-zk="' + k.index + '">Test</button>') + '</td>' +
          en + '</tr>';
      }).join('')
    : '<tr><td colspan="8" class="empty">No opencode keys configured</td></tr>';
  tb.onclick = e => {
    const b = e.target.closest('button[data-zk]');
    if (b) testZenKey(parseInt(b.dataset.zk, 10), b);
  };
  tb.onchange = e => {
    const c = e.target.closest('input[data-ze]');
    if (c) toggleZenKeyEnabled(parseInt(c.dataset.ze, 10), c.checked);
  };
}

async function toggleZenKeyEnabled(index, enabled) {
  try {
    await api('POST', '/opencode/keys/enabled', { index: index, enabled: enabled });
    toast(T('Key enabled state saved'), 'success');
  } catch (e) {
    toast('Save failed: ' + e.message, 'error');
    loadOcConfig();
  }
}

async function saveZenKeyProxy(index, input) {
  const row = input.closest('tr');
  const main = row.querySelector('.eg-input[data-slot="main"]').dataset.value;
  const backup = row.querySelector('.eg-input[data-slot="backup"]').dataset.value;
  try {
    await api('POST', '/opencode/keys/proxy', { index: index, main: main, backup: backup });
    toast(T('Key proxy binding saved'), 'success');
  } catch (e) {
    toast('Save failed: ' + e.message, 'error');
    loadOcConfig();
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
    _('ppProxies').value = (c.proxies || []).join('\n');
    _('ppStrategy').value = c.proxyStrategy || 'round_robin';
    _('ppCline').value = String(!!cfg.data.clineUseProxies);
    _('ppIsolation').value = String(c.proxyIsolation !== false);
    if (c.proxyIsolationEnvLocked) {
      _('ppIsolation').disabled = true;
      _('ppIsolationHint').innerHTML = '<span style="color:var(--warning,#eab308)">PROXY_ISOLATION env is set — it overrides this switch. Unset the env var to control isolation here.</span>';
    } else {
      _('ppIsolation').disabled = false;
      _('ppIsolationHint').textContent = '';
    }
    _('ppZen').value = String(c.zenUseProxies !== false);
    const cd = (c.runtime || {}).proxyCooldowns || {};
    const keys = Object.keys(cd);
    _('ppCooldownInfo').textContent = keys.length
      ? keys.map(k => egLabel(k) + T(' cooldown until ') + fmtWhen(cd[k])).join('; ')
      : T('No proxies cooling down');
  } catch (e) { /* ignore */ }
}

async function saveProxyPool() {
  const proxies = _('ppProxies').value.split('\n').map(s => s.trim()).filter(Boolean);
  const PROXY_RE = /^(https?|socks5h?|socks):\/\/[^\s]+:\d+/;
  const bad = proxies.find(p => !PROXY_RE.test(p));
  if (bad) { toast(T('Invalid proxy format: ') + bad + ' (need http(s)://host:port or socks5://host:port)', 'error'); return; }
  try {
    await api('POST', '/opencode/config/update', { proxies: proxies, proxyStrategy: _('ppStrategy').value });
    _('ppSaveResult').innerHTML = '<div style="color:var(--accent2);font-size:12px">' + T('Saved ') + proxies.length + T(' proxies') + '</div>';
    setTimeout(() => _('ppSaveResult').innerHTML = '', 5000);
    toast('Proxy pool saved', 'success');
    loadProxyPool();
  } catch (e) { toast(T('Save failed: ') + e.message, 'error'); }
}

async function saveClineProxies() {
  try {
    await api('POST', '/config/update', { clineUseProxies: _('ppCline').value === 'true' });
    toast('Cline proxy setting saved', 'success');
    loadProxyPool();
  } catch (e) { toast(T('Save failed: ') + e.message, 'error'); }
}

async function saveProxyIsolation() {
  try {
    await api('POST', '/opencode/config/update', { proxyIsolation: _('ppIsolation').value === 'true' });
    toast('Proxy isolation setting saved', 'success');
    loadProxyPool();
  } catch (e) { toast(T('Save failed: ') + e.message, 'error'); }
}

// loadOcModels 只维护探测模型下拉（模型列表本体已并入网关设置的可用模型区）。
async function loadOcModels() {
  try {
    const d = await api('GET', '/opencode/models');
    const models = d.data.models || [];
    const sel = _('ocProbeModel');
    if (sel) {
      const prev = sel.value;
      sel.innerHTML = '<option value="">' + T('auto — big-pickle first, then live models') + '</option>' +
        models.map(m => '<option value="' + esc(m.id) + '">' + esc(m.id) + (m.source === 'live' ? '' : T(' (seed)')) + '</option>').join('');
      if (Array.from(sel.options).some(o => o.value === prev)) sel.value = prev;
    }
  } catch (e) { /* ignore */ }
}

// ========== Live session IDs (zen FreeTier gate) ==========
// 未 mint 的 key 必 403；表格让"哪些 key 还是空会话"一眼可见，按钮给手动补收口。
let ocSessPoll = null;

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
  // 刷新节奏与并发写进提示行：用户据此判断"自动维护是否够用"，以及
  // 调 ZEN_HARVEST_INTERVAL_HOURS / ZEN_HARVEST_CONCURRENCY 要不要改。
  const h = _('ocSessHint');
  if (h) {
    const base = 'The free tier only accepts session IDs the upstream has actually seen, minted by the opencode CLI. A key without a live session <b>always</b> fails with 403.';
    h.innerHTML = base + (s.harvestEnabled
      ? ' Auto-refresh every <b>' + (s.intervalHours || 4) + 'h</b> (kept below the 5h quota window), minting <b>' +
        (s.concurrency || 1) + '</b> key(s) at a time' +
        ((s.concurrency || 1) === 1 ? ' (one after another — safest on small instances, the CLI is CPU/RAM hungry)' : '') + '.'
      : ' <b>Harvester unavailable</b> — no opencode CLI in this container (ZEN_HARVEST_BIN).');
  }
  // CLI 不在时后端会以 400 拒绝 mint，按钮先禁用，避免点了才发现。
  const usable = !!s.harvestEnabled;
  ['ocSessBtnMissing', 'ocSessBtnForce'].forEach(id => {
    const b = _(id);
    if (b) { b.disabled = !usable; b.title = usable ? '' : 'opencode CLI not available in this container'; }
  });
  _('ocSessBody').innerHTML = rows.length
    ? rows.map(k => {
        const state = k.noKey
          ? '<span style="color:var(--text2)">no key</span>'
          : (k.live
              ? '<span style="color:var(--accent2)">live</span>'
              : (k.minted ? '<span style="color:var(--danger)">stale</span>' : '<span style="color:var(--danger)">not minted</span>'));
        return '<tr><td>#' + (k.index + 1) + '</td>' +
          '<td style="font-family:monospace;font-size:11px">' + (k.session ? esc(k.session) + '…' : '-') + '</td>' +
          '<td>' + state + '</td>' +
          '<td style="font-size:12px">' + (k.harvested ? esc(fmtWhen(k.harvested)) : '-') + '</td></tr>';
      }).join('')
    : '<tr><td colspan="4" class="empty">No opencode keys configured</td></tr>';

  const j = s.job;
  if (!j) { _('ocSessTimer').textContent = ''; return; }
  if (j.running) {
    _('ocSessTimer').innerHTML = '<span style="color:var(--accent)">minting ' + (j.done || 0) + '/' + (j.total || 0) + '…</span>';
    renderOcMintResults(j.results || [], true);
  } else {
    _('ocSessTimer').textContent = T('last mint ') + fmtWhen(j.startedAt) + (j.force ? T(' (force)') : '');
    renderOcMintResults(j.results || [], false);
  }
}

function renderOcMintResults(results, running) {
  if (!results.length) { _('ocSessResult').innerHTML = ''; return; }
  const ok = results.filter(r => r.ok).length;
  const skipped = results.filter(r => r.skipped).length;
  const fail = results.filter(r => r.done && !r.ok && !r.skipped).length;
  const pending = results.filter(r => !r.done).length;
  const head = running
    ? '<span style="color:var(--accent)">' + T('Running — ') + ok + T(' minted, ') + fail + T(' failed, ') + pending + T(' pending') + '</span>'
    : (fail
        ? '<span style="color:var(--danger)">' + T('Done — ') + ok + T(' minted, ') + fail + T(' failed') + (skipped ? T(', ') + skipped + T(' skipped (already live)') : '') + '</span>'
        : '<span style="color:var(--accent2)">' + T('Done — ') + ok + T(' minted') + (skipped ? ', ' + skipped + T(' already live') : '') + '</span>');
  const details = results.filter(r => r.done && !r.ok && !r.skipped)
    .map(r => '#' + (r.index + 1) + ': ' + esc(r.error || 'failed')).join(' | ');
  _('ocSessResult').innerHTML = '<div style="font-size:12px">' + head + '</div>' +
    (details ? '<div class="hint" style="margin-top:4px">' + details + '</div>' : '');
}

async function mintZenSessions(force) {
  try {
    await api('POST', '/opencode/sessions/mint', { force: force });
    toast(force ? 'Force minting all sessions…' : 'Minting missing sessions…', 'success');
    // 任务在后端跑（串行 mint 时一批是 key 数 × 每 key 预算），按 2s 轮询进度。
    // 上限按后端批次预算推算：并发默认为 1 后 11 个 key 的批次上限可达几十分钟，
    // 写死 450 次（15 分钟）会让轮询提前停掉、进度条卡住不再更新。
    if (ocSessPoll) { clearInterval(ocSessPoll); ocSessPoll = null; }
    let ticks = 0;
    const s0 = await loadOcSessions();
    // 提前返回前必须把 ocSessPoll 置空：可见标签页的 20s 轮询用 !ocSessPoll
    // 判断"是否已有轮询在跑"，留一个已 clear 的非空句柄会让它永久停摆。
    if (!s0 || !s0.job || !s0.job.running) { ocSessPoll = null; return; }
    const j0 = s0.job;
    const workers = Math.max(1, s0.concurrency || 1);
    const perKey = s0.keyTimeoutSeconds || 150;
    // 下限 15 分钟与后端 harvestBatchTimeout 的下限对齐：key 少时后端照样跑满
    // 15 分钟，而 ceil(total/workers)×perKey 只有几分钟，不夹下限轮询会在后端
    // 放弃之前就停掉（之后只剩 20s 的可见标签页轮询，进度更新变粗）。
    const batchSeconds = Math.max(900, Math.ceil((j0.total || 1) / workers) * perKey + 120);
    const maxTicks = Math.min(1800, Math.ceil(batchSeconds / 2) + 30);
    ocSessPoll = setInterval(async () => {
      const s = await loadOcSessions();
      if (!s || !s.job || !s.job.running || ++ticks >= maxTicks) {
        clearInterval(ocSessPoll); ocSessPoll = null; loadOcConfig();
      }
    }, 2000);
  } catch (e) { toast(T('Mint failed: ') + e.message, 'error'); }
}

// ========== Combos (alias models) ==========
const comboModels = { cline: [], zen: [] };

async function fillComboModels() {
  const platform = _('comboPlatform').value;
  const sel = _('comboTarget');
  try {
    if (!comboModels[platform].length) {
      if (platform === 'cline') {
        const d = await api('GET', '/models');
        comboModels.cline = (d.data.models || []).map(m => m.id).filter(Boolean);
      } else {
        const d = await api('GET', '/opencode/models');
        comboModels.zen = (d.data.models || []).map(m => m.id).filter(Boolean);
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
        '<td><span class="model-tag">' + esc(c.platform === 'zen' ? 'opencode' : 'cline') + '</span></td>' +
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

async function saveZenProxies() {
  try {
    await api('POST', '/opencode/config/update', { zenUseProxies: _('ppZen').value === 'true' });
    toast(T('OpenCode proxy setting saved'), 'success');
    loadProxyPool();
  } catch (e) { toast(T('Save failed: ') + e.message, 'error'); }
}

async function loadOcStats() {
  try {
    const d = await api('GET', '/opencode/stats');
    const t = d.data.today || {}, s = d.data.total || {};
    _('ocStatsBox').innerHTML = '<table><thead><tr><th style="text-align:left"></th><th>Requests</th><th>Input tokens</th><th>Output tokens</th><th>Compaction</th><th>Rate-limited</th></tr></thead><tbody>' +
      '<tr><td style="text-align:left">Today</td><td>' + (t.requests || 0) + '</td><td>' + (t.promptTokens || 0) + '</td><td>' + (t.completionTokens || 0) + '</td><td>' + (t.compaction || 0) + '</td><td>' + (t.rateLimited || 0) + '</td></tr>' +
      '<tr><td style="text-align:left">Total</td><td>' + (s.requests || 0) + '</td><td>' + (s.promptTokens || 0) + '</td><td>' + (s.completionTokens || 0) + '</td><td>' + (s.compaction || 0) + '</td><td>' + (s.rateLimited || 0) + '</td></tr>' +
      '</tbody></table>';
    const bm = t.byModel || {};
    const rows = Object.keys(bm).map(k => '<tr><td style="text-align:left;font-family:monospace">' + esc(k) + '</td><td>' + bm[k].requests + '</td><td>' + bm[k].promptTokens + '</td><td>' + bm[k].completionTokens + '</td></tr>').join('');
    _('ocModelStatsBox').innerHTML = '<div style="font-size:13px;font-weight:600;margin-bottom:6px">Per-model breakdown (today)</div>' +
      '<div class="table-wrap"><table><thead><tr><th style="text-align:left">Model</th><th>Requests</th><th>Input tokens</th><th>Output tokens</th></tr></thead><tbody>' +
      (rows || '<tr><td colspan="4" style="text-align:left;color:var(--text2)">No data</td></tr>') + '</tbody></table></div>';
  } catch (e) { /* ignore */ }
}

// ========== Init ==========
loadStats();
loadAccounts();
loadKeys();
loadModels();
loadConfig();
setInterval(() => { loadStats(); }, 10000);
setInterval(() => { loadOcStats(); }, 15000);
// live 会话状态：只在 opencode 页可见时轮询（与日志页同样的省流约定）。
// mint 任务进行中由 mintZenSessions 自己的 2s 轮询接管。
setInterval(() => {
  if (_('tab-opencode').style.display !== 'none' && !ocSessPoll) loadOcSessions();
}, 20000);
setInterval(() => { if (logsAuto && _('tab-logs').style.display !== 'none') loadLogs(); }, 8000);
</script>
</body>
</html>`
