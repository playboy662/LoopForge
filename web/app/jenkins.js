function jenkinsOption(value,label){
  return `<option value="${escapeHTML(value)}">${escapeHTML(label)}</option>`;
}

function platformJenkinsHTML(){
  return paSection('Jenkins 连接','保存后，所有项目的负责人都可在「项目配置 → Jenkins 流水线」选择此连接并绑定 Job。',`
    <form data-jenkins-connection class="pa-rows">
      ${paRow('已有连接','选择已有连接可以修改或轮换令牌。','<select class="select" name="existing"><option value="">新增连接</option></select>')}
      ${paRow('连接名称','', '<input class="pa-input" name="displayName" required maxlength="120">')}
      ${paRow('Jenkins 地址','支持 HTTP 和 HTTPS。','<input class="pa-input" name="url" type="url" required placeholder="https://ci.example.com/jenkins">')}
      ${paRow('服务账号','', '<input class="pa-input" name="username" required autocomplete="off">')}
      ${paRow('API Token','修改地址或账号时需要重新填写。','<input class="pa-input" name="token" type="password" autocomplete="new-password">')}
      <p data-status role="status"></p><div><button class="button primary" type="submit">保存连接</button></div>
    </form>`);
}

async function loadPlatformJenkins(host){
  const form=host.querySelector('[data-jenkins-connection]');
  if(!form||form.dataset.loaded)return;
  form.dataset.loaded='true';
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
    status.textContent='';
  };
  form.onsubmit=async event=>{
    event.preventDefault();
    save.disabled=true;
    const id=fields.existing.value||uid('jenkins');
    const value={id,name:fields.displayName.value,url:fields.url.value,username:fields.username.value,
      token:fields.token.value,allProjects:true};
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
  catch(err){delete form.dataset.loaded;status.textContent=err.message}
}

function projectJenkinsHTML(){
  return paSection('Jenkins 流水线','按仓库和部署环境绑定 Job，供 SOP 引用。',`
    <div data-project-jenkins class="pa-rows">
      <div data-bindings></div>
      ${paRow('代码仓库','', '<select class="select" data-repository aria-label="代码仓库"></select>')}
      ${paRow('部署环境','', '<input class="pa-input" data-environment aria-label="部署环境" placeholder="例如 测试">')}
      ${paRow('Jenkins 连接','', '<select class="select" data-connection aria-label="Jenkins 连接"></select>')}
      ${paRow('Job','', '<select class="select" data-job aria-label="Jenkins Job"></select>')}
      <div data-job-parameters></div>
      <p data-status role="status"></p>
      <div><button type="button" class="button primary" data-save>添加绑定</button><button type="button" class="button ghost" data-reset hidden>取消编辑</button></div>
    </div>`);
}

function jenkinsBindingLabel(item){
  const repository=item.repositoryName||item.repositoryId||(item.service?`${item.service}（待补仓库）`:'待补仓库');
  return [repository,item.environment,item.connectionName||item.connectionId,item.job].join(' · ');
}

async function loadProjectJenkins(projectID){
  const host=$('#projectAdminForm [data-project-jenkins]');
  if(!host||host.dataset.loaded===projectID)return;
  host.dataset.loaded=projectID;
  const field=name=>host.querySelector('[data-'+name+']');
  const connection=field('connection'),job=field('job'),status=field('status'),save=field('save');
  const repository=field('repository'),environment=field('environment');
  const repositories=(state.data.repositories||[]).filter(item=>item.projectId===projectID);
  repository.innerHTML=jenkinsOption('','选择仓库')+repositories.map(item=>jenkinsOption(item.id,item.name)).join('');
  const base=`/api/projects/${encodeURIComponent(projectID)}/jenkins`;
  let bindings=[],selected='',revision=0;
  async function loadParameters(values={}){
    const request=++revision,box=field('job-parameters');
    box.innerHTML='';
    save.disabled=true;
    if(!job.value)return;
    try{
      const metadata=await api(base+'/jobs?'+new URLSearchParams({connectionId:connection.value,job:job.value}),{silent:true});
      if(request!==revision)return;
      const required=new Set(metadata.required);
      box.innerHTML=metadata.parameters.map((name,index)=>{
        const id='jenkins-param-'+index;
        return `<label class="pa-row" for="${id}"><span>${escapeHTML(name)}${required.has(name)?'（必填）':''}</span><input id="${id}" class="pa-input" data-job-parameter="${escapeHTML(name)}" value="${escapeHTML(values[name]??'')}" ${required.has(name)?'required':''}></label>`;
      }).join('');
      save.disabled=false;
      status.textContent='';
    }catch(err){if(request===revision)status.textContent=err.message}
  }
  function render(){
    field('bindings').innerHTML=bindings.length?bindings.map(item=>`<div class="pa-row">
      <div>${escapeHTML(jenkinsBindingLabel(item))}${item.unavailable?' · 绑定不可用':''}</div>
      <button type="button" class="button ghost small" data-edit-binding="${escapeHTML(item.id)}">编辑</button>
    </div>`).join(''):'<p>还没有绑定 Jenkins Job。</p>';
  }
  async function loadJobs(wanted='',values={}){
    const request=++revision,id=connection.value;
    job.innerHTML=jenkinsOption('','选择 Job');
    job.disabled=true;
    save.disabled=true;
    field('job-parameters').innerHTML='';
    if(!id)return;
    status.textContent='正在获取 Job…';
    try{
      const jobs=await api(base+'/jobs?'+new URLSearchParams({connectionId:id}),{silent:true});
      if(request!==revision)return;
      job.innerHTML=jenkinsOption('','选择 Job')+jobs.map(name=>jenkinsOption(name,name)).join('');
      job.value=jobs.includes(wanted)?wanted:'';
      job.disabled=false;
      status.textContent=jobs.length?'':'前 8 级目录内没有可构建的 Job';
      if(job.value)await loadParameters(values);
    }catch(err){if(request===revision)status.textContent=err.message}
  }
  connection.onchange=()=>loadJobs();
  job.onchange=()=>loadParameters();
  field('bindings').onclick=event=>{
    const item=bindings.find(row=>row.id===event.target.closest('[data-edit-binding]')?.dataset.editBinding);
    if(!item)return;
    selected=item.id;
    repository.value=item.repositoryId||'';
    environment.value=item.environment||'';
    save.textContent='保存绑定';
    field('reset').hidden=false;
    connection.value=item.connectionId;
    loadJobs(item.job,item.parameters||(item.parameter?{[item.parameter]:item.value}:{}));
  };
  field('reset').onclick=()=>{
    selected='';
    save.textContent='添加绑定';
    field('reset').hidden=true;
    repository.value='';
    environment.value='';
    job.value='';
    loadParameters();
  };
  save.onclick=async()=>{
    const item={id:selected||uid('job'),repositoryId:repository.value,environment:environment.value,connectionId:connection.value,job:job.value};
    const previous=bindings.find(row=>row.id===selected);
    if(previous?.service)item.service=previous.service;
    const inputs=[...host.querySelectorAll('[data-job-parameter]')];
    const missing=inputs.find(input=>input.required&&!input.value.trim());
    if(missing){status.textContent='请填写 '+missing.dataset.jobParameter;missing.focus();return}
    item.parameters=Object.fromEntries(inputs.filter(input=>input.value!=='').map(input=>[input.dataset.jobParameter,input.value]));
    if(!item.repositoryId||!item.job||!item.environment.trim()){status.textContent='请选择代码仓库和 Job，并填写部署环境';return}
    save.disabled=true;
    try{
      bindings=await api(base+'/bindings',{method:'PUT',body:JSON.stringify(item),silent:true});
      render();
      field('reset').click();
      status.textContent='Job 绑定已保存';
    }catch(err){status.textContent=err.message}
    finally{save.disabled=!job.value}
  };
  save.disabled=true;
  try{
    const [connections,items]=await Promise.all([api(base+'/connections',{silent:true}),api(base+'/bindings',{silent:true})]);
    bindings=items;
    render();
    connection.innerHTML=jenkinsOption('','选择连接')+connections.map(item=>jenkinsOption(item.id,item.name)).join('');
    status.textContent=!repositories.length?'请先在「代码仓库」中添加仓库':connections.length?'':'请让平台管理员在「Jenkins 连接」中新增或重新保存连接';
  }catch(err){status.textContent=err.message}
}

async function loadJenkinsNodeBindings(select,node){
  const projectID=state.workflow?.projectId||state.projectFilter;
  if(!projectID)return;
  try{
    const rows=await api(`/api/projects/${encodeURIComponent(projectID)}/jenkins/bindings`,{silent:true});
    if(!select.isConnected)return;
    const available=rows.filter(row=>!row.unavailable);
    select.innerHTML=jenkinsOption('','选择项目绑定')+available.map(row=>jenkinsOption(row.id,jenkinsBindingLabel(row))).join('');
    if(node.jenkinsBindingId&&!available.some(row=>row.id===node.jenkinsBindingId)){
      select.insertAdjacentHTML('beforeend',`<option value="${escapeHTML(node.jenkinsBindingId)}" disabled>已保存的绑定（当前不可用）</option>`);
    }
    select.value=node.jenkinsBindingId||'';
  }catch(err){
    if(select.isConnected)select.closest('.fd-field')?.querySelector('[data-jenkins-status]')?.replaceChildren(err.message);
  }
}
