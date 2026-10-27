package main

const webSettingsCSS = `
.settings-open{margin-left:auto;font-size:14px;letter-spacing:0}.top .dot{margin-left:14px}
#settingsDialog{padding:0;border:1px solid #555;border-radius:14px;background:#1b1b1b;color:#f5f5f5;width:min(900px,95vw);max-height:90vh;overflow:hidden;box-shadow:0 18px 80px #000a}
#settingsDialog::backdrop{background:#000b}#settingsDialog *{box-sizing:border-box}
.settings-header{padding:18px 22px;display:flex;align-items:center;justify-content:space-between;border-bottom:1px solid #444}.settings-header h2{margin:0;font-size:22px}
.settings-tabs{display:flex;gap:8px;flex-wrap:wrap;padding:14px 22px;border-bottom:1px solid #383838}.settings-tabs button[aria-selected=true]{background:#eee;color:#111}
.settings-scroll{overflow:auto;max-height:calc(90vh - 170px);padding:18px 22px 26px}.settings-panel[hidden]{display:none}.settings-panel h3{margin-top:0}
.settings-note,.setting-help{color:#aaa;font-size:13px;line-height:1.5}.settings-note{margin-top:0}.settings-note a{color:#ddd}
.setting-group,.source-card{border:1px solid #444;border-radius:9px;padding:14px;margin-bottom:14px;min-width:0}.setting-group legend{padding:0 8px;font-weight:600}.settings-grid{display:grid;grid-template-columns:1fr 1fr;gap:16px}
.setting-row{display:flex;flex-direction:column;gap:6px;min-width:0}.setting-row label{font-size:14px}.setting-row.check-row{display:block}.setting-row.check-row label{display:flex;gap:10px;align-items:center;min-height:36px}
#settingsDialog input:not([type=checkbox]),#settingsDialog select{display:block;width:100%;min-width:0;padding:10px;background:#282828;border:1px solid #555;border-radius:6px;color:#fff;font:inherit}
#settingsDialog input[type=checkbox]{width:18px;height:18px;accent-color:#ddd}#settingsDialog button{min-height:40px}#settingsDialog button:disabled{opacity:.5;cursor:wait}
.settings-actions{display:flex;gap:9px;align-items:center;flex-wrap:wrap;margin:14px 0}.settings-actions .primary{background:#eee;color:#111}.settings-status{font-size:14px;line-height:1.5;min-height:22px;white-space:pre-wrap}.settings-status.error{color:#ffb4b4}.settings-status.success{color:#c9e5cb}
.source-card summary{font-weight:600;cursor:pointer;padding:4px 0 14px;overflow-wrap:anywhere}.source-card .settings-status{margin-bottom:0}.source-remove{margin-left:auto}.settings-empty{color:#aaa;padding:18px 0}.settings-field-invalid{border-color:#ff9c9c!important}
@media(max-width:600px){#settingsDialog{width:96vw;max-height:94vh}.settings-grid{grid-template-columns:1fr}.settings-header,.settings-tabs{padding:12px 15px}.settings-scroll{padding:14px;max-height:calc(94vh - 168px)}.source-remove{margin-left:0}.settings-open{padding:8px 10px}}
`

const webSettingsJS = `
(()=>{
'use strict';
const dialog=document.createElement('dialog');dialog.id='settingsDialog';dialog.setAttribute('aria-labelledby','settingsTitle');
dialog.innerHTML='<header class="settings-header"><h2 id="settingsTitle">Settings</h2><button type="button" id="settingsClose" aria-label="Close settings">Close</button></header><nav class="settings-tabs" aria-label="Settings sections"><button type="button" data-settings-tab="app" aria-selected="true">App Settings</button><button type="button" data-settings-tab="sources" aria-selected="false">Source Settings</button></nav><div class="settings-scroll"><section id="appSettingsPanel" class="settings-panel"></section><section id="sourceSettingsPanel" class="settings-panel" hidden><nav class="settings-tabs" aria-label="Source settings"><button type="button" data-source-tab="plex" aria-selected="true">Plex</button><button type="button" data-source-tab="jellyfin" aria-selected="false">Jellyfin</button><button type="button" data-source-tab="emby" aria-selected="false">Emby</button><button type="button" data-source-tab="smb" aria-selected="false">SMB</button><button type="button" data-source-tab="radio" aria-selected="false">Online Radio</button></nav><section id="source-plex" class="settings-panel"></section><section id="source-jellyfin" class="settings-panel" hidden></section><section id="source-emby" class="settings-panel" hidden></section><section id="source-smb" class="settings-panel" hidden></section><section id="source-radio" class="settings-panel" hidden></section></section></div>';
document.body.appendChild(dialog);
const q=id=>document.getElementById(id);let loaded={};let fieldCounter=0;let activeAppTab="Playback";
const readPath=(obj,path)=>path.split('.').reduce((v,key)=>v?.[key],obj);
function writePath(obj,path,value){const keys=path.split('.');let node=obj;keys.slice(0,-1).forEach(key=>{node=node[key]||(node[key]={})});node[keys.at(-1)]=value}
function el(tag,text,cls){const node=document.createElement(tag);if(text!==undefined)node.textContent=text;if(cls)node.className=cls;return node}
function statusNode(parent){const node=el('p','','settings-status');node.setAttribute('role','status');parent.appendChild(node);return node}
function status(node,text,error=false){node.textContent=text;node.className='settings-status '+(error?'error':'success')}
async function post(url,body){return api(url,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)})}
function button(text,fn,cls=''){const node=el('button',text,cls);node.type='button';node.onclick=fn;return node}
function inputField(parent,definition,value){
 const row=el('div',undefined,'setting-row'+(definition.type==='checkbox'?' check-row':''));const id='setting-'+(++fieldCounter);const label=el('label',definition.label);label.htmlFor=id;let input;
 if(definition.type==='select'){input=el('select');(definition.options||[]).forEach(option=>{const node=el('option',option.label);node.value=String(option.value);input.appendChild(node)})}
 else{input=el('input');input.type=definition.type||'text'}
 input.id=id;input.dataset.path=definition.path||'';input.dataset.invert=definition.invert?'true':'false';input.dataset.numeric=(definition.type==='select'&&typeof definition.options?.[0]?.value==='number')?'true':'false';
 if(input.type==='checkbox'){input.checked=definition.invert?!value:!!value;label.prepend(input);row.appendChild(label)}else{input.value=value??'';row.append(label,input)}
 if(definition.type==='number'){input.min=definition.min??0;input.max=definition.max;input.step=definition.step||1}
 if(definition.type==='password'){input.autocomplete='new-password';input.placeholder=definition.placeholder||'Leave blank to keep saved value'}
 if(definition.required)input.required=true;
 if(definition.help)row.appendChild(el('span',definition.help,'setting-help'));
 parent.appendChild(row);return input;
}
function fieldValue(input){return input.type==='checkbox'?(input.dataset.invert==='true'?!input.checked:input.checked):(input.type==='number'||input.dataset.numeric==='true')?Number(input.value):input.value}
async function work(buttonNode,message,fn){buttonNode.disabled=true;status(message,'Testing…');try{const result=await fn();status(message,result.message||'Connection successful.')}catch(error){status(message,error.message,true)}finally{buttonNode.disabled=false}}
function selectSettings(tab){dialog.querySelectorAll('[data-settings-tab]').forEach(b=>b.setAttribute('aria-selected',b.dataset.settingsTab===tab));q('appSettingsPanel').hidden=tab!=='app';q('sourceSettingsPanel').hidden=tab!=='sources';if(tab==='app')loadApp();else loadSource('plex')}
function selectSource(tab){dialog.querySelectorAll('[data-source-tab]').forEach(b=>b.setAttribute('aria-selected',b.dataset.sourceTab===tab));['plex','jellyfin','emby','smb','radio'].forEach(name=>q('source-'+name).hidden=name!==tab);loadSource(tab)}
dialog.querySelectorAll('[data-settings-tab]').forEach(b=>b.onclick=()=>selectSettings(b.dataset.settingsTab));dialog.querySelectorAll('[data-source-tab]').forEach(b=>b.onclick=()=>selectSource(b.dataset.sourceTab));
q('settingsOpen').onclick=()=>{loaded={};q('appSettingsPanel').replaceChildren();['plex','jellyfin','emby','smb','radio'].forEach(name=>q('source-'+name).replaceChildren());dialog.showModal();selectSource('plex');selectSettings('app')};q('settingsClose').onclick=()=>dialog.close();
async function loadApp(force=false){
 if(loaded.app&&!force)return;loaded.app=true;const panel=q('appSettingsPanel');panel.replaceChildren(el('p','Loading app settings…','settings-note'));
 try{
 const data=await api('/api/settings/app');panel.replaceChildren(el('p','Changes use the same configuration as the MiSTer app. Web Remote enabled/port changes require restarting Hi-Fi.','settings-note'));
 const form=el('form');panel.appendChild(form);const groups=new Map(),inputs=new Map();let initial=data.config;
 const categoryNames=['Playback','Appearance','Controls','System','Experimental'];
 const categoryNav=el('nav',undefined,'settings-tabs');categoryNav.setAttribute('role','tablist');categoryNav.setAttribute('aria-label','App settings categories');form.appendChild(categoryNav);
 const categoryPanels=new Map(),categoryButtons=new Map();
 function chooseCategory(name,focus=false){activeAppTab=name;categoryPanels.forEach((node,key)=>node.hidden=key!==name);categoryButtons.forEach((node,key)=>{node.setAttribute('aria-selected',key===name);node.tabIndex=key===name?0:-1});if(focus)categoryButtons.get(name).focus()}
 for(const name of categoryNames){const id=name.toLowerCase(),page=el('section');page.id='app-category-'+id;page.setAttribute('role','tabpanel');page.setAttribute('aria-labelledby','app-tab-'+id);categoryPanels.set(name,page);const b=button(name,()=>chooseCategory(name));b.id='app-tab-'+id;b.setAttribute('role','tab');b.setAttribute('aria-controls',page.id);b.onkeydown=e=>{let index=categoryNames.indexOf(name);if(e.key==='ArrowLeft')index=(index+4)%5;else if(e.key==='ArrowRight')index=(index+1)%5;else if(e.key==='Home')index=0;else if(e.key==='End')index=4;else return;e.preventDefault();chooseCategory(categoryNames[index],true)};categoryNav.appendChild(b);categoryButtons.set(name,b);form.appendChild(page)}
 function category(field){if(['fps_target','gba_music_enabled','gapless_playback'].includes(field.path))return 'Experimental';if(['confirm_on_exit','show_web_address','web_remote_enabled','web_remote_port'].includes(field.path))return 'System';if(field.group==='Display')return 'Appearance';if(field.group==='Controls and navigation')return 'Controls';return 'Playback'}
 for(const field of data.fields){
  const section=category(field),groupKey=section+':'+field.group;let grid=groups.get(groupKey);if(!grid){const group=el('fieldset',undefined,'setting-group');group.appendChild(el('legend',field.group));grid=el('div',undefined,'settings-grid');group.appendChild(grid);categoryPanels.get(section).appendChild(group);groups.set(groupKey,grid)}
  const input=inputField(grid,field,readPath(initial,field.path));inputs.set(field.path,input);
 }
 const art=inputs.get('hide_album_art'),autoArt=inputs.get('auto_hide_missing_art');const artState=()=>{autoArt.disabled=!art.checked};art.onchange=artState;artState();
 chooseCategory(categoryNames.includes(activeAppTab)?activeAppTab:'Playback');
 const eqGrid=groups.get('Playback:Equalizer');const presetSelect=inputField(eqGrid,{type:'select',label:'Preset',options:[{value:'custom',label:'Custom'},...data.presets.map((p,i)=>({value:String(i),label:p.Name}))]},'custom');
 function syncPreset(){const match=data.presets.findIndex(p=>['Bass','LowMid','Mid','HighMid','Treble'].every(k=>Number(inputs.get('eq.'+k).value)===p[k]));presetSelect.value=match<0?'custom':String(match)}
 presetSelect.onchange=()=>{if(presetSelect.value==='custom')return;const preset=data.presets[Number(presetSelect.value)];['Bass','LowMid','Mid','HighMid','Treble'].forEach(k=>inputs.get('eq.'+k).value=preset[k]);inputs.get('eq.enabled').checked=true};['Bass','LowMid','Mid','HighMid','Treble'].forEach(k=>inputs.get('eq.'+k).addEventListener('input',syncPreset));syncPreset();
 const eqActions=el('div',undefined,'settings-actions');eqActions.appendChild(button('Reset flat',()=>{['Bass','LowMid','Mid','HighMid','Treble'].forEach(k=>inputs.get('eq.'+k).value=0);syncPreset()}));eqGrid.parentNode.appendChild(eqActions);
 const actions=el('div',undefined,'settings-actions');const save=el('button','Save app settings','primary');save.type='submit';actions.append(save,button('Reload',()=>loadApp(true)));form.appendChild(actions);const message=statusNode(form);
 form.onsubmit=async event=>{event.preventDefault();if(!form.reportValidity())return;const patch={};
 for(const [path,input]of inputs){const value=fieldValue(input);if(input.type==='password'&&value==='')continue;if(value!==readPath(initial,path))writePath(patch,path,value)}
 if(!Object.keys(patch).length){status(message,'No changes to save.');return}
 save.disabled=true;try{const result=await post('/api/settings/app',patch);initial=result.config;for(const[path,input]of inputs){if(input.type==='password')input.value=''}status(message,result.restart_required?'Saved. Restart Hi-Fi to apply the Web Remote changes.':'App settings saved and applied.');if(Object.hasOwn(patch,'gba_music_enabled')){await sources();if(!result.config.gba_music_enabled&&currentSource==='gba'){currentSource='';currentPath='';$('path').textContent='';$('list').replaceChildren(el('div','GBA Music is disabled. Select another source.','empty'))}}}catch(error){status(message,error.message,true)}finally{save.disabled=false}};
 }catch(error){loaded.app=false;panel.replaceChildren(el('p',error.message,'settings-status error'),button('Retry',()=>loadApp(true)))}
}
async function refreshSources(){await sources();if(currentSource==='radio'||currentSource==='plex'||currentSource==='jellyfin'||currentSource==='emby'){await browse(currentPath)}else if(currentSource.startsWith('smb:')){currentSource='';currentPath='';$('list').replaceChildren(el('div','Source settings saved. Select a source to browse.','empty'))}}
async function loadSource(kind,force=false){
 if(loaded[kind]&&!force)return;loaded[kind]=true;const panel=q('source-'+kind);panel.replaceChildren(el('p','Loading source settings…','settings-note'));
 try{if(kind==='plex')await plexForm(panel);else if(kind==='jellyfin'||kind==='emby')await jellyfinForm(panel,kind);else await collectionForm(panel,kind)}catch(error){loaded[kind]=false;panel.replaceChildren(el('p',error.message,'settings-status error'),button('Retry',()=>loadSource(kind,true)))}
}
async function plexForm(panel){
 const data=await api('/api/plex/config'),c=data.config;panel.replaceChildren(el('h3','Plex'));
 panel.appendChild(el('p','Enter the local server URL and X-Plex-Token. Leave the library ID blank to use the first music library. Blank token keeps the saved token for the same server.','settings-note'));
 const form=el('form'),grid=el('div',undefined,'settings-grid');form.appendChild(grid);panel.appendChild(form);
 const inputs={};for(const field of[
 {path:'server',label:'Server URL',type:'url',required:true},{path:'token',label:'X-Plex-Token',type:'password',placeholder:data.configured?'Saved token — leave blank to keep':'Enter your X-Plex-Token'},
 {path:'library',label:'Music library ID (optional)',type:'text',help:'Numeric section ID, not the library name.'},
 {path:'max_track_mb',label:'Maximum track size (MB)',type:'number',min:1,max:1024,step:1},
 {path:'scrobbling',label:'Scrobbling',type:'select',options:[{value:'plex',label:'Plex (external Last.fm scrobbler)'},{value:'lastfm',label:'Hi-Fi Last.fm'},{value:'listenbrainz',label:'Hi-Fi ListenBrainz'},{value:'both',label:'Hi-Fi Last.fm + ListenBrainz'},{value:'none',label:'None'}]},
 {path:'insecure',label:'Allow self-signed HTTPS certificate',type:'checkbox'}]){inputs[field.path]=inputField(grid,field,c[field.path])}
 const read=()=>Object.fromEntries(Object.entries(inputs).map(([key,input])=>[key,fieldValue(input)]));
 const actions=el('div',undefined,'settings-actions'),message=statusNode(form);const save=el('button','Save Plex settings','primary');save.type='submit';
 const test=button('Test connection',()=>{if(form.reportValidity())work(test,message,()=>post('/api/settings/test',{kind:'plex',plex:read()}))});actions.append(save,test,button('Reload',()=>loadSource('plex',true)));form.insertBefore(actions,message);
 form.onsubmit=async event=>{event.preventDefault();if(!form.reportValidity())return;save.disabled=true;try{await post('/api/plex/config',read());inputs.token.value='';inputs.token.placeholder='Saved token — leave blank to keep';status(message,'Plex settings saved.');await refreshSources()}catch(error){status(message,error.message,true)}finally{save.disabled=false}};
}
async function jellyfinForm(panel,kind='jellyfin'){
 const emby=kind==='emby',N=emby?'Emby':'Jellyfin',data=await api('/api/'+kind+'/config'),c=data.config;panel.replaceChildren(el('h3',N));
 panel.appendChild(el('p',(emby?'Enter the server URL and save it, then sign in with your Emby user name and password: the password is not stored, only a token. ':'Enter the server URL and save it, then sign in with Quick Connect: no password is stored, only a token. ')+'Leave the library ID blank to use the first music library. '+N+' appears in the sources once it is set up.','settings-note'));
 const form=el('form'),grid=el('div',undefined,'settings-grid');form.appendChild(grid);panel.appendChild(form);
 const inputs={};for(const field of[
 {path:'server',label:'Server URL',type:'url',required:true,placeholder:'http://192.168.1.10:8096'},
 {path:'library',label:'Music library ID (optional)',type:'text',help:'The library id from '+N+', not its name.'},
 {path:'max_track_mb',label:'Maximum track size (MB)',type:'number',min:1,max:1024,step:1},
 {path:'scrobbling',label:'Scrobbling',type:'select',options:[{value:'server',label:N+' (it keeps the history)'},{value:'lastfm',label:'Hi-Fi Last.fm'},{value:'listenbrainz',label:'Hi-Fi ListenBrainz'},{value:'both',label:'Hi-Fi Last.fm + ListenBrainz'},{value:'none',label:'None'}]},
 {path:'insecure',label:'Allow self-signed HTTPS certificate',type:'checkbox'}]){inputs[field.path]=inputField(grid,field,c[field.path])}
 const read=()=>Object.fromEntries(Object.entries(inputs).map(([key,input])=>[key,fieldValue(input)]));
 const actions=el('div',undefined,'settings-actions'),message=statusNode(form);const save=el('button','Save '+N+' settings','primary');save.type='submit';
 const test=button('Test connection',()=>{if(form.reportValidity())work(test,message,()=>post('/api/settings/test',emby?{kind:'emby',emby:read()}:{kind:'jellyfin',jellyfin:read()}))});
 actions.append(save,test,button('Reload',()=>loadSource(kind,true)));form.insertBefore(actions,message);
 form.onsubmit=async event=>{event.preventDefault();if(!form.reportValidity())return;save.disabled=true;try{const r=await post('/api/'+kind+'/config',read());status(message,r.signed_in?N+' settings saved.':'Settings saved. Now sign in'+(emby?'.':' with Quick Connect.'));await refreshSources();if(r.signed_in!==data.signed_in)await loadSource(kind,true)}catch(error){status(message,error.message,true)}finally{save.disabled=false}};
 const box=el('div',undefined,'source-card');box.appendChild(el('h4',data.signed_in?'Signed in':'Not signed in'));
 const login=el('p','','settings-status');box.appendChild(login);
 let btn;
 if(emby){
  const ui=document.createElement('input');ui.type='text';ui.placeholder='User name';ui.autocomplete='username';
  const pi=document.createElement('input');pi.type='password';pi.placeholder='Password';pi.autocomplete='current-password';
  box.append(ui,pi);
  btn=button(data.signed_in?'Sign in again':'Sign in',async()=>{btn.disabled=true;try{await post('/api/emby/signin',{user:ui.value,password:pi.value});pi.value='';login.className='settings-status success';login.textContent='Signed in.';await refreshSources();await loadSource('emby',true)}catch(error){login.className='settings-status error';login.textContent=error.message}finally{btn.disabled=false}});
 }else{
  btn=button(data.signed_in?'Sign in again':'Sign in with Quick Connect',async()=>{
  btn.disabled=true;try{const r=await post('/api/jellyfin/quickconnect',{action:'start'});login.className='settings-status';login.textContent='In Jellyfin open your profile, Quick Connect, and enter the code: '+r.code;
   const poll=async()=>{const s=await api('/api/jellyfin/quickconnect');if(s.state==='pending'){setTimeout(poll,2000)}else if(s.state==='signed_in'){login.className='settings-status success';login.textContent='Signed in.';btn.disabled=false;await refreshSources();await loadSource('jellyfin',true)}else{login.className='settings-status error';login.textContent=s.message||'Sign-in failed.';btn.disabled=false}};setTimeout(poll,2000)
  }catch(error){login.className='settings-status error';login.textContent=error.message;btn.disabled=false}});
 }
 box.appendChild(btn);panel.appendChild(box);
}
async function collectionForm(panel,kind){
 const isSMB=kind==='smb',title=isSMB?'SMB shares':'Online Radio',arrayKey=isSMB?'shares':'stations';const data=await api('/api/settings/'+kind);let entries=data[arrayKey]||[];
 panel.replaceChildren(el('h3',title));panel.appendChild(el('p',isSMB?'Add network shares using the server name or IP, share name and optional subfolder. Blank saved passwords are retained. Changes apply when you browse the source again.':'Add direct HTTP(S) audio stream URLs. Changes apply when you select a station again.','settings-note'));
 const form=el('form'),cards=el('div');form.appendChild(cards);panel.appendChild(form);const records=[];
 function addCard(entry={},open=true){
  const card=el('details',undefined,'source-card');card.open=open;const summary=el('summary',isSMB?(entry.name||'New SMB share'):(entry.Name||entry.name||'New radio station'));card.appendChild(summary);const grid=el('div',undefined,'settings-grid');card.appendChild(grid);const inputs={};
  const fields=isSMB?[
   {path:'name',label:'Display name',type:'text'},{path:'server',label:'Server name or IP',type:'text',required:true},{path:'share',label:'Share name',type:'text',required:true},{path:'path',label:'Subfolder (optional)',type:'text'},{path:'username',label:'Username',type:'text'},{path:'password',label:'Password',type:'password',placeholder:entry.has_password?'Saved password — leave blank to keep':'Password (if required)'},{path:'guest',label:'Guest access',type:'checkbox'},{path:'clear_password',label:'Clear saved password',type:'checkbox'}
  ]:[{path:'name',label:'Station name',type:'text',required:true},{path:'url',label:'Direct stream URL',type:'url',required:true},{path:'genre',label:'Genre (optional)',type:'text'}];
  for(const field of fields){const value=entry[field.path]??entry[{name:'Name',url:'URL',genre:'Genre'}[field.path]];inputs[field.path]=inputField(grid,field,value)}
  inputs.name.addEventListener('input',()=>summary.textContent=inputs.name.value||(isSMB?'New SMB share':'New radio station'));
  if(isSMB){const guestState=()=>{inputs.username.disabled=inputs.guest.checked;inputs.password.disabled=inputs.guest.checked;inputs.clear_password.disabled=inputs.guest.checked};inputs.guest.onchange=guestState;guestState()}
  const read=()=>{const result=Object.fromEntries(Object.entries(inputs).map(([key,input])=>[key,fieldValue(input)]));if(isSMB)result.id=entry.id||'';return result};
  const record={card,read};records.push(record);cards.appendChild(card);const actions=el('div',undefined,'settings-actions'),message=statusNode(card);
  const test=button(isSMB?'Test connection':'Test station',()=>{let valid=true;for(const input of Object.values(inputs)){if(!input.reportValidity())valid=false}if(valid)work(test,message,()=>post('/api/settings/test',{kind,revision:data.revision,[isSMB?'smb':'radio']:read()}))});
  const up=button('Move up',()=>{const index=records.indexOf(record);if(index>0){[records[index-1],records[index]]=[records[index],records[index-1]];cards.insertBefore(card,card.previousElementSibling)}});
  const down=button('Move down',()=>{const index=records.indexOf(record);if(index<records.length-1){[records[index],records[index+1]]=[records[index+1],records[index]];cards.insertBefore(card.nextElementSibling,card)}});
  const remove=button('Remove',()=>{records.splice(records.indexOf(record),1);card.remove();empty.hidden=records.length>0},'source-remove');actions.append(test,up,down,remove);card.insertBefore(actions,message);return record;
 }
 const empty=el('p',isSMB?'No SMB shares configured.':'No radio stations configured.','settings-empty');cards.appendChild(empty);
 entries.forEach(entry=>addCard(entry,false));empty.hidden=records.length>0;
 const actions=el('div',undefined,'settings-actions'),save=el('button',isSMB?'Save SMB shares':'Save radio stations','primary');save.type='submit';actions.append(button(isSMB?'Add share':'Add station',()=>{const record=addCard();empty.hidden=true;record.card.querySelector('input').focus()}),save,button('Reload',()=>loadSource(kind,true)));form.appendChild(actions);const message=statusNode(form);
 form.onsubmit=async event=>{event.preventDefault();if(!form.reportValidity())return;save.disabled=true;try{await post('/api/settings/'+kind,{revision:data.revision,[arrayKey]:records.map(r=>r.read())});await refreshSources();await loadSource(kind,true);const newMessage=q('source-'+kind).querySelector('form > .settings-status');status(newMessage,'Settings saved.')}catch(error){status(message,error.message,true)}finally{save.disabled=false}};
}
})();
`
