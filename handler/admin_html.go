package handler

const adminHTML = `<!DOCTYPE html>
<html lang="zh">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Codex2API 管理</title>
<style>
  body{font-family:system-ui,sans-serif;max-width:680px;margin:40px auto;padding:0 20px;background:#f5f5f5}
  h1{color:#333;display:flex;align-items:center;justify-content:space-between}
  .card{background:#fff;border-radius:8px;padding:24px;margin:16px 0;box-shadow:0 1px 3px rgba(0,0,0,.1)}
  h2{margin-top:0;font-size:16px;color:#444}
  label{display:block;margin-bottom:4px;font-weight:600;color:#555;font-size:13px}
  input,textarea{width:100%;box-sizing:border-box;padding:8px 12px;border:1px solid #ddd;border-radius:4px;font-size:14px;margin-bottom:10px}
  textarea{height:140px;font-family:monospace;font-size:12px}
  .row{display:flex;gap:8px}
  button{background:#0070f3;color:#fff;border:none;padding:9px 18px;border-radius:4px;cursor:pointer;font-size:13px;white-space:nowrap}
  button:hover{background:#0051cc}
  .btn-gray{background:#6c757d} .btn-gray:hover{background:#545b62}
  .btn-red{background:#dc3545} .btn-red:hover{background:#b02a37}
  .btn-sm{padding:3px 10px;font-size:12px}
  .msg{padding:9px 12px;border-radius:4px;margin-top:10px;display:none;font-size:13px}
  .ok{background:#d4edda;color:#155724} .err{background:#f8d7da;color:#721c24}
  .keys-list{font-family:monospace;font-size:12px;background:#f8f8f8;padding:10px;border-radius:4px;word-break:break-all;margin-top:8px}
  .toggle{display:flex;align-items:center;gap:8px;margin-bottom:10px}
  .toggle input{width:auto;margin:0}
  small{color:#888;font-size:12px;display:block;margin-top:-6px;margin-bottom:8px}
  #login-section{display:none}
  #main-section{display:none}
  #logout-btn{background:#6c757d;font-size:12px;padding:5px 12px}
  #logout-btn:hover{background:#545b62}
</style>
</head>
<body>
<h1>Codex2API 管理面板 <button id="logout-btn" onclick="logout()" style="display:none">退出登录</button></h1>

<!-- 初始化密码（仅未设置时显示） -->
<div id="init-section" class="card" style="display:none">
  <h2>设置管理员密码</h2>
  <label>密码（至少 8 位）</label>
  <input type="password" id="init-pw" placeholder="首次使用请设置密码">
  <button onclick="initPassword()">设置密码</button>
  <div id="init-msg" class="msg"></div>
</div>

<!-- 登录 -->
<div id="login-section" class="card">
  <h2>登录</h2>
  <label>管理员密码</label>
  <input type="password" id="login-pw" placeholder="输入密码" onkeydown="if(event.key==='Enter')login()">
  <button onclick="login()">登录</button>
  <div id="login-msg" class="msg"></div>
</div>

<!-- 登录后内容 -->
<div id="main-section">

<div class="card">
  <h2>上传 auth.json</h2>
  <label>粘贴 auth.json 内容</label>
  <textarea id="auth-json" placeholder='{"auth_mode":"chatgpt","tokens":{"access_token":"...","refresh_token":"...","account_id":"..."}}'></textarea>
  <button onclick="uploadAuth()">上传</button>
  <div id="upload-msg" class="msg"></div>
</div>

<div class="card">
  <h2>API Key 管理</h2>
  <div class="row">
    <button onclick="generateKey()">生成新 Key</button>
    <button class="btn-gray" onclick="listKeys()">刷新列表</button>
  </div>
  <div id="key-msg" class="msg"></div>
  <div id="keys-list" class="keys-list" style="display:none"></div>
</div>

<div class="card">
  <h2>高级配置</h2>
  <button class="btn-gray" onclick="loadConfig()" style="margin-bottom:12px">加载当前配置</button>

  <label>Codex 接入点 (codex_base)</label>
  <small>默认: https://chatgpt.com/backend-api/codex</small>
  <input type="text" id="cfg-codex-base" placeholder="留空使用默认值">

  <label>Token 刷新地址 (refresh_url)</label>
  <small>默认: https://auth.openai.com/oauth/token</small>
  <input type="text" id="cfg-refresh-url" placeholder="留空使用默认值">

  <label>OAuth Client ID</label>
  <small>默认: app_EMoamEEZ73f0CkXa**hr***</small>
  <input type="text" id="cfg-client-id" placeholder="留空使用默认值">

  <label>模型列表</label>
  <small>用 | 分隔多个模型，第一个将作为默认模型。留空使用内置列表。</small>
  <input type="text" id="cfg-models-list" placeholder="例: gpt-5.2|gpt-5.4">

  <label>默认模型（仅在未设置模型列表时生效）</label>
  <small>默认: gpt-5.2</small>
  <input type="text" id="cfg-model" placeholder="留空使用默认值">

  <div class="toggle">
    <input type="checkbox" id="cfg-local-auth">
    <label for="cfg-local-auth" style="margin:0;font-weight:normal">启用本地 auth.json 读取（~/.codex/auth.json）</label>
  </div>
  <small style="margin-top:-4px">开启后无需手动上传授权文件，直接读取本机 Codex CLI 的认证信息</small>

  <button onclick="saveConfig()">保存配置</button>
  <div id="cfg-msg" class="msg"></div>
</div>

<div class="card">
  <h2>修改密码</h2>
  <label>旧密码</label>
  <input type="password" id="old-pw">
  <label>新密码（至少 8 位）</label>
  <input type="password" id="new-pw">
  <label>确认新密码</label>
  <input type="password" id="new-pw2">
  <button onclick="changePassword()">修改密码</button>
  <div id="chpw-msg" class="msg"></div>
</div>

</div><!-- /main-section -->

<script>
async function post(path,body){
  const r=await fetch(path,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});
  return{ok:r.ok,status:r.status,data:await r.json()};
}
function show(id,ok,msg){
  const el=document.getElementById(id);
  el.className='msg '+(ok?'ok':'err');
  el.textContent=msg;
  el.style.display='block';
}

async function checkState(){
  const{data}=await post('/admin/check-session',{});
  if(data.logged_in){
    showMain();
    listKeys();
  } else {
    // check if password is set by trying a dummy login
    const r=await post('/admin/login',{password:''});
    if(r.status===401){
      // password is set, show login
      document.getElementById('login-section').style.display='block';
    } else {
      // no password set yet
      document.getElementById('init-section').style.display='block';
    }
  }
}

function showMain(){
  document.getElementById('login-section').style.display='none';
  document.getElementById('init-section').style.display='none';
  document.getElementById('main-section').style.display='block';
  document.getElementById('logout-btn').style.display='inline-block';
}

async function initPassword(){
  const pw=document.getElementById('init-pw').value;
  if(!pw)return show('init-msg',false,'请输入密码');
  const{ok,data}=await post('/admin/init-password',{password:pw});
  show('init-msg',ok,data.message??data.error);
  if(ok){
    document.getElementById('init-section').style.display='none';
    document.getElementById('login-section').style.display='block';
  }
}

async function login(){
  const pw=document.getElementById('login-pw').value;
  if(!pw)return show('login-msg',false,'请输入密码');
  const{ok,data}=await post('/admin/login',{password:pw});
  if(ok){
    showMain();
    listKeys();
  } else {
    show('login-msg',false,data.error);
  }
}

async function logout(){
  await post('/admin/logout',{});
  location.reload();
}

async function uploadAuth(){
  const json=document.getElementById('auth-json').value;
  if(!json)return show('upload-msg',false,'请填写 auth.json 内容');
  let parsed;
  try{parsed=JSON.parse(json)}catch{return show('upload-msg',false,'auth.json 格式错误')}
  const{ok,data}=await post('/admin/upload-auth',{auth:parsed});
  show('upload-msg',ok,data.message??data.error);
  if(data.error==='未登录')location.reload();
}

async function generateKey(){
  const{ok,data}=await post('/admin/generate-key',{});
  show('key-msg',ok,ok?'新 Key: '+data.key:data.error);
  if(ok)listKeys();
  if(data.error==='未登录')location.reload();
}

async function listKeys(){
  const{ok,data}=await post('/admin/list-keys',{});
  if(!ok){show('key-msg',false,data.error);if(data.error==='未登录')location.reload();return;}
  const el=document.getElementById('keys-list');
  el.style.display='block';
  if(!data.keys||!data.keys.length){el.textContent='（暂无 Key）';return;}
  el.innerHTML=data.keys.map(k=>
    '<div style="display:flex;justify-content:space-between;align-items:center;padding:4px 0;border-bottom:1px solid #eee">'+
    '<span>'+k+'</span>'+
    '<button class="btn-red btn-sm" onclick="deleteKey(\''+k+'\')">删除</button>'+
    '</div>'
  ).join('');
}

async function deleteKey(key){
  const{ok,data}=await post('/admin/delete-key',{key});
  show('key-msg',ok,data.message??data.error);
  if(ok)listKeys();
}

async function loadConfig(){
  const{ok,data}=await post('/admin/get-config',{});
  if(!ok){show('cfg-msg',false,data.error);if(data.error==='未登录')location.reload();return;}
  document.getElementById('cfg-codex-base').value=data.codex_base||'';
  document.getElementById('cfg-refresh-url').value=data.refresh_url||'';
  document.getElementById('cfg-client-id').value=data.client_id||'';
  document.getElementById('cfg-models-list').value=data.models_list||'';
  document.getElementById('cfg-model').value=data.default_model||'';
  document.getElementById('cfg-local-auth').checked=!!data.local_auth_enabled;
  show('cfg-msg',true,'配置已加载');
}

async function saveConfig(){
  const payload={
    codex_base:document.getElementById('cfg-codex-base').value,
    refresh_url:document.getElementById('cfg-refresh-url').value,
    client_id:document.getElementById('cfg-client-id').value,
    models_list:document.getElementById('cfg-models-list').value,
    default_model:document.getElementById('cfg-model').value,
    local_auth_enabled:document.getElementById('cfg-local-auth').checked,
  };
  const{ok,data}=await post('/admin/set-config',payload);
  show('cfg-msg',ok,data.message??data.error);
  if(data.error==='未登录')location.reload();
}

async function changePassword(){
  const oldPw=document.getElementById('old-pw').value;
  const newPw=document.getElementById('new-pw').value;
  const newPw2=document.getElementById('new-pw2').value;
  if(!oldPw||!newPw)return show('chpw-msg',false,'请填写所有字段');
  if(newPw!==newPw2)return show('chpw-msg',false,'两次新密码不一致');
  const{ok,data}=await post('/admin/change-password',{old_password:oldPw,new_password:newPw});
  show('chpw-msg',ok,data.message??data.error);
  if(ok){
    document.getElementById('old-pw').value='';
    document.getElementById('new-pw').value='';
    document.getElementById('new-pw2').value='';
  }
}

checkState();
</script>
</body>
</html>`
