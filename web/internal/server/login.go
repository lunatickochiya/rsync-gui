package server

const loginPage = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>rsync web · 需要访问令牌</title>
<style>
:root{color-scheme:dark}
*{box-sizing:border-box}
body{margin:0;min-height:100vh;display:grid;place-items:center;background:#0a0a0a;color:#ededed;font:15px/1.6 ui-sans-serif,system-ui,-apple-system,"Segoe UI",sans-serif}
main{width:min(420px,calc(100vw - 32px));padding:32px;border:1px solid #2e2e2e;border-radius:18px;background:#111}
.mark{color:#fab283;font-size:13px;letter-spacing:.08em;text-transform:uppercase}
h1{font-size:24px;margin:8px 0 6px}
p{color:#8a8a8a;margin:0 0 22px}
label{display:block;color:#b9b9b9;font-size:13px;margin-bottom:8px}
input{width:100%;padding:12px 14px;border:1px solid #2e2e2e;border-radius:10px;background:#0a0a0a;color:#ededed;font:inherit;outline:none}
input:focus{border-color:#fab283}
button{margin-top:14px;width:100%;padding:12px 14px;border:0;border-radius:10px;background:#fab283;color:#24160f;font:600 15px/1 inherit;cursor:pointer}
button:hover{background:#ffc09f}
.hint{margin-top:18px;font-size:12px;color:#4c4c4c;line-height:1.7}
</style>
</head>
<body>
<main>
<div class="mark">rsync web</div>
<h1>需要访问令牌</h1>
<p>这个服务监听在可被远程访问的地址上，请输入启动时终端显示的令牌。</p>
<form method="get" action="/">
<label for="token">访问令牌</label>
<input id="token" name="token" type="password" autocomplete="current-password" autofocus required>
<button type="submit">进入传输面板</button>
</form>
<div class="hint">令牌会保存在 HttpOnly Cookie 中。局域网明文 HTTP 只建议在可信网络使用，公网请配合 SSH 隧道或 HTTPS 反向代理。</div>
</main>
</body>
</html>
`
