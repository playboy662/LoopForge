function jenkinsOption(value,label){
  return `<option value="${escapeHTML(value)}">${escapeHTML(label)}</option>`;
}

function platformJenkinsHTML(){
  const projects=(state.data.projects||[]).filter(p=>!p.archivedAt);
  return paSection('Jenkins 连接','为项目授权 Jenkins 服务账号。项目负责人随后可从该账号可见的 Job 中选择。',`
    <form data-jenkins-connection class="pa-rows">
      ${paRow('已有连接','选择已有连接可以修改或轮换令牌。','<select class="select" name="existing"><option value="">新增连接</option></select>')}
      ${paRow('连接名称','', '<input class="pa-input" name="displayName" required maxlength="120">')}
      ${paRow('Jenkins 地址','包含 Jenkins 的访问路径。','<input class="pa-input" name="url" type="url" required placeholder="https://ci.example.com/jenkins">')}
      ${paRow('服务账号','', '<input class="pa-input" name="username" required autocomplete="off">')}
      ${paRow('API Token','修改地址或账号时需要重新填写。','<input class="pa-input" name="token" type="password" autocomplete="new-password">')}
      ${paRow('授权项目','可选多个项目。','<select class="select" name="projectIds" multiple aria-label="授权项目">'+projects.map(p=>jenkinsOption(p.id,p.name)).join('')+'</select>')}
      <p data-status role="status"></p><div><button class="button primary" type="submit">保存连接</button></div>
    </form>`);
}

async function loadPlatformJenkins(host){
  const form=host.querySelector('[data-jenkins-connection]');
  if(!form)return;
  const fields=form.elements,status=form.querySelector('[data-status]'),save=form.querySelector('[type=submit]');
  const endpoint='/api/platform/jenkins';
  let items=[];
  function showList(id){
    fields.existing.innerHTML=jenkinsOption('','新增连接')+items.map(item=>jenkinsOption(item.id,item.name)).join('');
    fields.existing.value=id;
  }
  fields.existing.onchange=()=>{
    const item=items.find(row=>row.id===fields.existing.value);
    fields.displayName.value=item?.name||'';
    fields.url.value=item?.url||'';
    fields.username.value=item?.username||'';
    fields.token.value='';
    for(const option of fields.projectIds.options)option.selected=item?.projectIds?.includes(option.value)||false;
    status.textContent='';
  };
  form.onsubmit=async event=>{
    event.preventDefault();
    save.disabled=true;
    const id=fields.existing.value||uid('jenkins');
    const value={id,name:fields.displayName.value,url:fields.url.value,username:fields.username.value,
      token:fields.token.value,projectIds:Array.from(fields.projectIds.selectedOptions,o=>o.value)};
    try{
      items=await api(endpoint,{method:'PUT',body:JSON.stringify(value),silent:true});
      fields.token.value='';
      showList(id);
      status.textContent='连接已保存';
    }catch(err){status.textContent=err.message}
    finally{save.disabled=false}
  };
  save.disabled=true;
  try{items=await api(endpoint,{silent:true});showList('');save.disabled=false}
  catch(err){status.textContent=err.message}
}

function projectJenkinsHTML(){
  return paSection('Jenkins 流水线','按服务和环境选择已有 Job，SOP 可直接引用。',`
    <div data-project-jenkins class="pa-rows">
      <div data-bindings></div>
      ${paRow('Jenkins 连接','', '<select class="select" data-connection aria-label="Jenkins 连接"></select>')}
      ${paRow('Job','', '<select class="select" data-job aria-label="Jenkins Job"></select>')}
      ${paRow('服务','', '<input class="pa-input" data-service aria-label="服务" placeholder="例如 后端">')}
      ${paRow('部署环境','', '<input class="pa-input" data-environment aria-label="部署环境" placeholder="例如 测试">')}
      ${paRow('Job 参数（可选）','填写 Jenkins Job 使用的实际参数名和固定值。','<div class="pw-row2"><input class="pa-input" data-parameter aria-label="Job 参数名" placeholder="BRANCH"><input class="pa-input" data-value aria-label="Job 参数值" placeholder="test"></div>')}
      <p data-status role="status"></p>
      <div><button type="button" class="button primary" data-save>添加绑定</button><button type="button" class="button ghost" data-reset hidden>取消编辑</button></div>
    </div>`);
}

async function loadProjectJenkins(projectID){
  const host=$('#projectAdminForm [data-project-jenkins]');
  if(!host||host.dataset.loaded===projectID)return;
  host.dataset.loaded=projectID;
  const field=name=>host.querySelector('[data-'+name+']');
  const connection=field('connection'),job=field('job'),status=field('status'),save=field('save');
  const base=`/api/projects/${encodeURIComponent(projectID)}/jenkins`;
  let bindings=[],selected='',revision=0;
  function render(){
    field('bindings').innerHTML=bindings.length?bindings.map(item=>`<div class="pa-row">
      <div><b>${escapeHTML(item.environment)} · ${escapeHTML(item.service)}</b><div>${escapeHTML(item.job)}</div></div>
      <button type="button" class="button ghost small" data-edit-binding="${escapeHTML(item.id)}">编辑</button>
    </div>`).join(''):'<p>还没有绑定 Jenkins Job。</p>';
  }
  async function loadJobs(wanted=''){
    const request=++revision,id=connection.value;
    job.innerHTML=jenkinsOption('','选择 Job');
    job.disabled=true;
    save.disabled=true;
    if(!id)return;
    status.textContent='正在获取 Job…';
    try{
      const jobs=await api(base+'/jobs?'+new URLSearchParams({connectionId:id}),{silent:true});
      if(request!==revision)return;
      job.innerHTML=jenkinsOption('','选择 Job')+jobs.map(name=>jenkinsOption(name,name)).join('');
      job.value=jobs.includes(wanted)?wanted:'';
      job.disabled=false;
      save.disabled=false;
      status.textContent=jobs.length?'':'当前账号没有可构建的 Job';
    }catch(err){if(request===revision)status.textContent=err.message}
  }
  connection.onchange=()=>loadJobs();
  field('bindings').onclick=event=>{
    const item=bindings.find(row=>row.id===event.target.closest('[data-edit-binding]')?.dataset.editBinding);
    if(!item)return;
    selected=item.id;
    for(const key of ['service','environment','parameter','value'])field(key).value=item[key]||'';
    save.textContent='保存绑定';
    field('reset').hidden=false;
    connection.value=item.connectionId;
    loadJobs(item.job);
  };
  field('reset').onclick=()=>{
    selected='';
    save.textContent='添加绑定';
    field('reset').hidden=true;
    for(const key of ['service','environment','parameter','value'])field(key).value='';
    job.value='';
  };
  save.onclick=async()=>{
    const item={id:selected||uid('job'),connectionId:connection.value,job:job.value};
    for(const key of ['service','environment','parameter','value'])item[key]=field(key).value;
    if(!item.job||!item.service.trim()||!item.environment.trim()){status.textContent='请选择 Job，并填写服务和环境';return}
    save.disabled=true;
    try{
      bindings=await api(base+'/bindings',{method:'PUT',body:JSON.stringify(item),silent:true});
      render();
      field('reset').click();
      status.textContent='Job 绑定已保存';
    }catch(err){status.textContent=err.message}
    finally{save.disabled=false}
  };
  save.disabled=true;
  try{
    const [connections,items]=await Promise.all([api(base+'/connections',{silent:true}),api(base+'/bindings',{silent:true})]);
    bindings=items;
    render();
    connection.innerHTML=jenkinsOption('','选择连接')+connections.map(item=>jenkinsOption(item.id,item.name)).join('');
    status.textContent=connections.length?'':'请让平台管理员为当前项目授权 Jenkins 连接';
  }catch(err){status.textContent=err.message}
}

async function loadJenkinsNodeBindings(select,node){
  const projectID=state.workflow?.projectId||state.projectFilter;
  if(!projectID)return;
  try{
    const rows=await api(`/api/projects/${encodeURIComponent(projectID)}/jenkins/bindings`,{silent:true});
    if(!select.isConnected)return;
    select.innerHTML=jenkinsOption('','选择项目绑定')+rows.map(row=>jenkinsOption(row.id,`${row.environment} · ${row.service} · ${row.job}`)).join('');
    if(node.jenkinsBindingId&&!rows.some(row=>row.id===node.jenkinsBindingId)){
      select.insertAdjacentHTML('beforeend',jenkinsOption(node.jenkinsBindingId,'已保存的绑定（当前不可见）'));
    }
    select.value=node.jenkinsBindingId||'';
  }catch(err){
    if(select.isConnected)select.closest('.fd-field')?.querySelector('[data-jenkins-status]')?.replaceChildren(err.message);
  }
}
