package httpapi

import "net/http"

const dashboardHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>宠物门店预约</title>
<style>
:root{color-scheme:light;--ink:#17202a;--muted:#68737d;--line:#d9dee3;--accent:#176b87;--danger:#a13d3d}
*{box-sizing:border-box}body{margin:0;background:#f5f7f8;color:var(--ink);font:15px/1.5 system-ui,-apple-system,"Segoe UI",sans-serif}
main{max-width:1100px;margin:0 auto;padding:28px 20px 56px}header{display:flex;justify-content:space-between;gap:16px;align-items:center;border-bottom:1px solid var(--line);padding-bottom:18px;margin-bottom:22px}h1{font-size:24px;margin:0}h2{font-size:17px;margin:0 0 12px}section{background:#fff;border:1px solid var(--line);padding:18px;margin-bottom:16px}label{display:grid;gap:5px;color:var(--muted);font-size:13px}input,button{font:inherit;padding:9px 10px;border:1px solid #bac4cc;border-radius:4px}button{background:var(--accent);border-color:var(--accent);color:#fff;cursor:pointer}button.secondary{background:#fff;color:var(--accent)}form.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:12px;align-items:end}.actions{display:flex;flex-wrap:wrap;gap:8px;margin-top:12px}.hidden{display:none}.notice{padding:10px 12px;background:#eef5f7;border-left:3px solid var(--accent);margin-bottom:16px;white-space:pre-wrap}.error{color:var(--danger)}table{border-collapse:collapse;width:100%;font-size:13px}th,td{text-align:left;border-bottom:1px solid var(--line);padding:9px 7px;vertical-align:top}th{color:var(--muted);font-weight:600}.meta{color:var(--muted);font-size:13px}.split{display:grid;grid-template-columns:1fr 1fr;gap:16px}@media(max-width:700px){header{align-items:flex-start;flex-direction:column}.split{grid-template-columns:1fr}}
</style></head>
<body><main>
<header><h1>宠物门店预约</h1><div id="identity" class="meta">未登录</div></header>
<div id="notice" class="notice">请使用预置账号登录。</div>
<section id="loginPanel"><h2>登录</h2><form id="loginForm" class="grid"><label>账号<input name="username" autocomplete="username" required></label><label>密码<input name="password" type="password" autocomplete="current-password" required></label><button>登录</button></form></section>
<div id="app" class="hidden">
<section><div class="split"><div><h2>查询日期</h2><form id="queryForm" class="grid"><label>业务日期<input id="date" type="date" required></label><button>查询预约</button><button type="button" class="secondary" id="daily">查询日清</button></form></div><div><h2>预约操作</h2><form id="actionForm" class="grid"><label>预约编号<input id="bookingId" type="number" min="1" required></label><label>原因<input id="reason" placeholder="取消申请或未到店原因"></label><button type="button" data-action="cancel-request">申请取消</button><button type="button" data-action="check-in">核销</button><button type="button" data-action="service/start">开始服务</button><button type="button" data-action="service/complete">完成服务</button><button type="button" data-action="no-show">标记未到店</button></form></div></div></section>
<section><h2>结果</h2><div id="result" class="meta">尚未查询</div></section>
</div></main>
<script>
const $=s=>document.querySelector(s), notice=(text,bad=false)=>{$('#notice').textContent=text;$('#notice').className=bad?'notice error':'notice'};
const json=async(r)=>{const t=await r.text();let x;try{x=JSON.parse(t)}catch{ x=t }if(!r.ok)throw new Error(typeof x==='string'?x:(x.error||t||'HTTP '+r.status));return x};
async function refresh(){const me=await json(await fetch('/me'));$('#identity').textContent=me.role+' · 账号 '+me.id;$('#loginPanel').classList.add('hidden');$('#app').classList.remove('hidden');}
$('#loginForm').addEventListener('submit',async e=>{e.preventDefault();try{const r=await fetch('/login',{method:'POST',body:new URLSearchParams(new FormData(e.target))});await json(r);await refresh();notice('登录成功')}catch(err){notice(err.message,true)}});
$('#queryForm').addEventListener('submit',async e=>{e.preventDefault();try{const day=$('#date').value;const x=await json(await fetch('/bookings?date='+encodeURIComponent(day)));$('#result').textContent=JSON.stringify(x,null,2);notice('已加载 '+x.bookings.length+' 条预约')}catch(err){notice(err.message,true)}});
$('#daily').addEventListener('click',async()=>{try{const day=$('#date').value;const x=await json(await fetch('/daily-summary?date='+encodeURIComponent(day)));$('#result').textContent=JSON.stringify(x,null,2);notice('已加载日清')}catch(err){notice(err.message,true)}});
document.querySelectorAll('[data-action]').forEach(b=>b.addEventListener('click',async()=>{try{const id=$('#bookingId').value;if(!id)throw new Error('请输入预约编号');const action=b.dataset.action;const body=new URLSearchParams();if(action==='cancel-request'||action==='no-show')body.set('reason',$('#reason').value);const r=await fetch('/bookings/'+id+'/'+action,{method:'POST',body});const x=await json(r);$('#result').textContent=JSON.stringify(x,null,2);notice('操作成功')}catch(err){notice(err.message,true)}}));
</script></body></html>`

func serveDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(dashboardHTML))
}
