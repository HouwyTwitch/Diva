const $=s=>document.querySelector(s), video=$('#screen'), status=$('#status'), panel=$('#connect');
let pc,dc,ws;
$('#start').onclick=connect; $('#fullscreen').onclick=()=>video.requestFullscreen();
async function connect(){
 const room=$('#room').value.trim(), token=$('#token').value; if(!room||!token)return;
 localStorage.setItem('diva-room',room); status.textContent='Подключение…';
 const config=await fetch('/config.json',{cache:'no-store'}).then(r=>r.json()).catch(()=>({stunUrl:''}));
 ws=new WebSocket(`${location.protocol==='https:'?'wss':'ws'}://${location.host}/ws?role=viewer&room=${encodeURIComponent(room)}&token=${encodeURIComponent(token)}`);
 pc=new RTCPeerConnection({iceServers:config.stunUrl?[{urls:config.stunUrl}]:[]}); pc.ontrack=e=>video.srcObject=e.streams[0]; pc.onicecandidate=e=>e.candidate&&send('candidate',e.candidate); pc.onconnectionstatechange=()=>{status.textContent=pc.connectionState; if(pc.connectionState==='connected')panel.hidden=true;if(pc.connectionState==='failed'){panel.hidden=false;status.textContent='ICE failed';panel.querySelector('p').textContent='Не удалось установить UDP-соединение. Проверьте проброс UDP 50000, Windows Firewall, PublicIP и CGNAT.'}};
 const candidates=[]; ws.onmessage=async e=>{const m=JSON.parse(e.data); try{if(m.type==='offer'){await pc.setRemoteDescription(m.payload); for(const c of candidates)await pc.addIceCandidate(c); candidates.length=0; const a=await pc.createAnswer(); await pc.setLocalDescription(a); send('answer',a)}else if(m.type==='candidate'){if(pc.remoteDescription)await pc.addIceCandidate(m.payload);else candidates.push(m.payload)}else if(m.type==='host-left')location.reload()}catch(err){console.error('signaling',err);status.textContent='Ошибка WebRTC'}};
 pc.ondatachannel=e=>{dc=e.channel; installInput()}; ws.onerror=()=>status.textContent='Ошибка соединения';
}
function send(type,payload){ws.send(JSON.stringify({type,payload}))}
function installInput(){ video.tabIndex=0; const emit=(type,e)=>{if(dc?.readyState!=='open')return; e.preventDefault(); const box=video.getBoundingClientRect(),scale=Math.min(box.width/video.videoWidth,box.height/video.videoHeight),w=video.videoWidth*scale,h=video.videoHeight*scale,left=(box.width-w)/2,top=(box.height-h)/2; dc.send(JSON.stringify({type,key:e.code,button:e.button,x:Math.max(0,Math.min(1,(e.clientX-box.left-left)/w)),y:Math.max(0,Math.min(1,(e.clientY-box.top-top)/h)),dx:e.deltaX,dy:e.deltaY}))}; ['keydown','keyup','mousedown','mouseup','mousemove','wheel'].forEach(t=>video.addEventListener(t,e=>emit(t,e))); video.oncontextmenu=e=>e.preventDefault(); video.onclick=()=>video.focus() }
$('#room').value=localStorage.getItem('diva-room')||'';
