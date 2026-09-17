const $=s=>document.querySelector(s), video=$('#screen'), status=$('#status'), panel=$('#connect');
let pc,dc,ws;
$('#start').onclick=connect; $('#fullscreen').onclick=()=>video.requestFullscreen();
async function connect(){
 const room=$('#room').value.trim(), token=$('#token').value; if(!room||!token)return;
 localStorage.setItem('diva-room',room); status.textContent='Подключение…';
 ws=new WebSocket(`${location.protocol==='https:'?'wss':'ws'}://${location.host}/ws?role=viewer&room=${encodeURIComponent(room)}&token=${encodeURIComponent(token)}`);
 pc=new RTCPeerConnection({iceServers:[]}); pc.ontrack=e=>video.srcObject=e.streams[0]; pc.onicecandidate=e=>e.candidate&&send('candidate',e.candidate); pc.onconnectionstatechange=()=>{status.textContent=pc.connectionState; if(pc.connectionState==='connected')panel.hidden=true};
 ws.onmessage=async e=>{const m=JSON.parse(e.data); if(m.type==='offer'){await pc.setRemoteDescription(m.payload); const a=await pc.createAnswer(); await pc.setLocalDescription(a); send('answer',a)}else if(m.type==='candidate')await pc.addIceCandidate(m.payload);else if(m.type==='host-left')location.reload()};
 pc.ondatachannel=e=>{dc=e.channel; installInput()}; ws.onerror=()=>status.textContent='Ошибка соединения';
}
function send(type,payload){ws.send(JSON.stringify({type,payload}))}
function installInput(){ video.tabIndex=0; const emit=(type,e)=>{if(dc?.readyState!=='open')return; e.preventDefault(); dc.send(JSON.stringify({type,key:e.code,button:e.button,x:e.offsetX/video.clientWidth,y:e.offsetY/video.clientHeight,dx:e.deltaX,dy:e.deltaY}))}; ['keydown','keyup','mousedown','mouseup','mousemove','wheel'].forEach(t=>video.addEventListener(t,e=>emit(t,e))); video.onclick=()=>video.focus() }
$('#room').value=localStorage.getItem('diva-room')||'';
