'use strict';
const app = document.querySelector('#app');
const nav = document.querySelector('#nav');
const notice = document.querySelector('#notice');
const typeNames = {single:'单选题',multiple:'多选题',text:'填空题',number:'数值题'};
const statusNames = {draft:'草稿',published:'已发布',closed:'已关闭'};
let user = null, noticeTimer, routeGeneration = 0;
function el(tag, attrs = {}, ...children) {
  const node = document.createElement(tag);
  for (const [key,value] of Object.entries(attrs)) {
    if (key.startsWith('on')) node.addEventListener(key.slice(2),value);
    else if (key === 'class') node.className = value;
    else if (key === 'text') node.textContent = value;
    else if (key in node) node[key] = value;
    else node.setAttribute(key,value);
  }
  for (const child of children.flat()) if (child !== null && child !== undefined) node.append(child instanceof Node ? child : document.createTextNode(String(child)));
  return node;
}
function show(message, success = false) {
  clearTimeout(noticeTimer); notice.textContent = message; notice.className = success ? 'success' : ''; notice.hidden = false;
  noticeTimer = setTimeout(() => { notice.hidden = true; },success ? 4500 : 12000);
}
notice.onclick = () => { notice.hidden = true; };
async function api(path, method = 'GET', body) {
  const response = await fetch(path,{method,headers:body === undefined ? {} : {'Content-Type':'application/json'},body:body === undefined ? undefined : JSON.stringify(body)});
  const data = await response.json().catch(() => null);
  if (!response.ok) throw new Error(data?.error || `请求失败（${response.status}）`);
  return data;
}
function button(label, handler, className = '') {
  return el('button',{type:'button',class:className,onclick:async event => {
    const target = event.currentTarget; target.disabled = true;
    try { await handler(); } catch (error) { show(error.message); } finally { target.disabled = false; }
  }},label);
}
function field(label, input, help) { return el('label',{class:'field'},el('span',{},label),input,help ? el('p',{class:'help'},help) : null); }
function checkbox(label, checked, callback) { return el('label',{class:'check'},el('input',{type:'checkbox',checked,onchange:event => callback(event.target.checked)}),label); }
function input(value, callback, type = 'text', attrs = {}) { return el('input',{type,value:value ?? '',oninput:event => callback(event.target.value),...attrs}); }
function select(options, value, callback) {
  const node = el('select',{onchange:event => callback(event.target.value)},options.map(([key,label]) => el('option',{value:key},label))); node.value = value; return node;
}
function link(label, path, className = '') { return el('a',{href:path,class:className,onclick:event => {event.preventDefault();go(path);}},label); }
function go(path) { history.pushState({},'',path); render(); }
window.addEventListener('popstate',render);
document.querySelectorAll('[data-nav]').forEach(a => a.onclick = event => {event.preventDefault();go(a.getAttribute('href'));});
function updateNav() {
  nav.replaceChildren(link('我的问卷','/'));
  if (user) nav.append(el('span',{class:'username muted'},user.username),button('退出',async () => {await api('/api/logout','POST');user=null;render();}));
  else nav.append(link('登录 / 注册','/login?return='+encodeURIComponent(location.pathname)));
}
function hero(title, subtitle, action) { return el('section',{class:'hero'},el('div',{},el('h1',{},title),el('p',{class:'muted'},subtitle)),action); }
async function render() {
  const generation = ++routeGeneration; notice.hidden = true; updateNav(); app.replaceChildren(el('p',{class:'loading'},'正在加载…'));
  const path = location.pathname;
  try {
    let view;
    if (path === '/login') view = authView();
    else if (/^\/survey\/[^/]+$/.test(path)) view = await fillView(path.split('/')[2]);
    else if (!user) view = authView();
    else if (/^\/edit\/[^/]+$/.test(path)) view = await editorView(path.split('/')[2]);
    else if (/^\/stats\/[^/]+$/.test(path)) view = await statsView(path.split('/')[2]);
    else view = await dashboard();
    if (generation !== routeGeneration) return;
    app.replaceChildren(view); window.scrollTo(0,0);
  } catch(error) {
    if (generation !== routeGeneration) return;
    app.replaceChildren(el('section',{class:'card'},el('h1',{},'暂时无法打开'),el('p',{class:'error'},error.message),link('返回我的问卷','/')));
  }
}
function authView() {
  let registering = false;
  const username = input('',() => {},'text',{required:true,autocomplete:'username',minLength:3,maxLength:32});
  const password = input('',() => {},'password',{required:true,autocomplete:'current-password',minLength:8});
  const title = el('h1',{},'欢迎回来');
  const submit = el('button',{class:'primary',type:'submit'},'登录');
  const toggle = button('没有账号？注册',() => {registering=!registering;title.textContent=registering?'创建账号':'欢迎回来';submit.textContent=registering?'注册并登录':'登录';toggle.textContent=registering?'已有账号？登录':'没有账号？注册';password.autocomplete=registering?'new-password':'current-password';});
  const form = el('form',{class:'card stack',onsubmit:async event => {
    event.preventDefault();submit.disabled=true;
    try {
      const payload={username:username.value.trim(),password:password.value};
      user=await api(registering?'/api/register':'/api/login','POST',payload);
      // Registration and login share the same session contract.
      const destination=new URLSearchParams(location.search).get('return') || (location.pathname !== '/login' ? location.pathname : '/');
      go(destination.startsWith('/') && !destination.startsWith('//') ? destination : '/');
    } catch(error) {show(error.message);} finally {submit.disabled=false;}
  }},field('用户名',username,'3–32 位字母、数字或下划线'),field('密码',password,'至少 8 个字符'),submit,toggle);
  return el('section',{class:'auth'},title,el('p',{class:'muted'},'登录后可以创建、填写问卷，以及查看自己创建问卷的统计结果。'),form);
}
async function dashboard() {
  const surveys=await api('/api/surveys');
  let archivedView=false;
  const root=el('div',{},hero('我的问卷','创建、发布问卷并查看填写结果。',button('＋ 新建问卷',async()=>{
    const survey=await api('/api/surveys','POST',{title:'未命名问卷',description:'',allowAnonymous:true,allowRepeat:false,deadline:null,questions:[]});
    go('/edit/'+survey.id);
  },'primary')));
  const toggle=button('查看已归档问卷',()=>{
    archivedView=!archivedView;
    toggle.textContent=archivedView?'返回全部未归档问卷':'查看已归档问卷';
    paintList();
  });
  const list=el('div');
  root.append(el('div',{class:'actions'},toggle),list);
  function paintList(){
    const visible=surveys.filter(s=>!!s.archived===archivedView);
    if(!visible.length){
      list.replaceChildren(el('div',{class:'empty'},el('h2',{},archivedView?'没有已归档的问卷':'还没有问卷'),el('p',{class:'muted'},archivedView?'可以在问卷详情中归档或恢复问卷。':'点击“新建问卷”开始。')));
      return;
    }
    list.replaceChildren(el('div',{class:'grid'},visible.map(survey=>
      el('article',{class:'card'},
        el('div',{class:'row between'},
          el('span',{class:'badge '+survey.status},survey.archived?'已归档 · '+(statusNames[survey.status]||survey.status):(statusNames[survey.status]||survey.status)),
          el('span',{class:'help'},`${survey.questions?.length||0} 道题`)),
        el('h2',{class:'preview'},survey.title),
        el('p',{class:'muted preview'},survey.description||'暂无说明'),
        el('p',{class:'help'},survey.deadline?'截止：'+new Date(survey.deadline).toLocaleString():'未设置截止时间'),
        el('div',{class:'actions'},
          link(survey.status==='draft'&&!survey.archived?'编辑问卷':'查看问卷','/edit/'+survey.id),
          survey.status!=='draft'?link('查看统计','/stats/'+survey.id):null)))));
  }
  paintList();
  return root;
}
function identifier(prefix) {return prefix+'_'+Math.random().toString(36).slice(2,10);}
async function editorView(id) {
  let survey=await api('/api/surveys/'+id);
  let busy=false;
  const root=el('fieldset',{style:'border:0;padding:0;margin:0;min-width:0'});

  async function operation(path,method='POST',body){
    if(busy)return;
    busy=true;root.disabled=true;root.setAttribute('aria-busy','true');
    try{
      const result=await api('/api/surveys/'+id+path,method,body);
      if(method==='DELETE'){go('/');return;}
      survey=result;
      paint();
    }finally{
      busy=false;root.disabled=false;root.setAttribute('aria-busy','false');
    }
  }
  async function save(publish=false){
    if(busy)return;
    busy=true;root.disabled=true;root.setAttribute('aria-busy','true');
    try{
      survey=await api('/api/surveys/'+id,'PUT',survey);
      if(publish)survey=await api('/api/surveys/'+id+'/publish','POST');
      show(publish?'发布成功，可以分享填写链接了':'草稿已保存',true);
    }finally{
      busy=false;root.disabled=false;root.setAttribute('aria-busy','false');paint();
    }
  }
  function paint() {
    const locked=survey.status!=='draft'||!!survey.archived;
    root.replaceChildren(hero(survey.archived?'已归档问卷':locked?'问卷详情':'设计问卷',
      survey.archived?'恢复归档后可以继续管理问卷。':locked?'发布后的题目和规则不可修改。':'先设置题目与规则，再保存并发布。',
      link('← 我的问卷','/')));
    const title=input(survey.title,value=>survey.title=value);
    const description=el('textarea',{value:survey.description,oninput:event=>survey.description=event.target.value});
    let localDeadline='';
    if(survey.deadline){
      const date=new Date(survey.deadline);
      localDeadline=new Date(date.getTime()-date.getTimezoneOffset()*60000).toISOString().slice(0,16);
    }
    const deadline=input(localDeadline,value=>survey.deadline=value?new Date(value).toISOString():null,'datetime-local');
    root.append(el('fieldset',{class:'card',disabled:locked},
      field('问卷标题',title),field('问卷说明',description),
      el('div',{class:'grid'},
        field('截止时间（本地时区）',deadline),
        el('div',{class:'stack'},checkbox('允许匿名填写',survey.allowAnonymous,value=>survey.allowAnonymous=value),
          checkbox('允许重复提交',survey.allowRepeat,value=>survey.allowRepeat=value))),
      el('p',{class:'help'},'填写者均须登录；不允许重复时，同一账号（包含匿名填写）只能提交一次。')));
    if(survey.status!=='draft'){
      const url=location.origin+'/survey/'+id;
      root.append(el('section',{class:'card'},
        el('div',{class:'row between'},el('h2',{},'分享与统计'),
          el('span',{class:'badge '+survey.status},statusNames[survey.status])),
        el('div',{class:'share'},el('input',{value:url,readOnly:true,'aria-label':'问卷分享链接'}),
          button('复制链接',async()=>{
            if(navigator.clipboard)await navigator.clipboard.writeText(url);
            else throw new Error('请选中上方链接后复制。');
            show('链接已复制',true);
          })),
        el('div',{class:'actions'},link('打开填写页面','/survey/'+id),link('查看回答统计','/stats/'+id))));
    }
    if(survey.archived){
      root.append(el('section',{class:'card'},
        el('h2',{},'归档管理'),
        el('p',{class:'help'},'归档不删除题目或答卷。恢复后可继续管理；如原来已发布，归档时已经自动关闭。'),
        button('恢复归档',()=>operation('/restore'))));
    }else{
      const actions=[];
      if(survey.status==='draft'){
        actions.push(button('删除草稿',async()=>{
          if(typeof window.confirm==='function'&&!window.confirm('确定永久删除此草稿？该操作无法撤销。'))return;
          await operation('','DELETE');
        },'danger'));
      }
      if(survey.status==='published'){
        actions.push(button('关闭问卷',()=>operation('/close'),'danger'));
      }
      if(survey.status==='closed'){
        const initial=survey.deadline&&new Date(survey.deadline)>new Date()?localDeadline:'';
        const nextDeadline=input(initial,()=>{},'datetime-local',{'aria-label':'重新开启后的截止时间'});
        const reopen=el('div',{class:'stack'},
          field('重新开启后的截止时间（可留空）',nextDeadline,
            '留空表示取消截止时间；若使用原截止时间，它必须尚未到期。重新开启不会清除已有回答。'),
          button('重新开启问卷',()=>{
            const raw=nextDeadline.value;
            const deadline=raw?new Date(raw).toISOString():null;
            return operation('/reopen','POST',{deadline});
          },'primary'));
        root.append(el('section',{class:'card'},el('h2',{},'重新开启'),reopen));
      }
      actions.push(button('归档问卷',async()=>{
        const warning=survey.status==='published'
          ?'归档会同时关闭正在收集回答的问卷，已有回答将保留。确定归档？'
          :'归档将从默认列表隐藏问卷，已有数据会保留。确定归档？';
        if(typeof window.confirm==='function'&&!window.confirm(warning))return;
        await operation('/archive');
      }));
      root.append(el('section',{class:'card'},el('h2',{},'问卷管理'),
        el('p',{class:'help'},'仅草稿允许永久删除。已发布的问卷可关闭或归档，保留已有答卷。'),
        el('div',{class:'actions'},actions)));
    }
    survey.questions.forEach((question,index)=>root.append(questionEditor(question,index,locked)));
    if(!locked){
      const addType=select(Object.entries(typeNames),'single',()=>{});
      root.append(el('section',{class:'card row'},addType,button('＋ 添加题目',()=>{
        const type=addType.value;
        survey.questions.push({id:identifier('q'),title:'',type,required:true,
          options:['single','multiple'].includes(type)?[{id:identifier('o'),label:'选项 1'},{id:identifier('o'),label:'选项 2'}]:[],
          validation:{integer:false},rules:[]});
        paint();
      })));
      root.append(el('div',{class:'toolbar row between'},
        el('span',{class:'help'},`${survey.questions.length} 道题 · 修改后请保存`),
        el('div',{class:'actions'},button('保存草稿',()=>save()),button('保存并发布',()=>save(true),'primary'))));
    }
  }
  function questionEditor(question,index,locked){
    question.validation ||= {};question.rules ||= [];question.options ||= [];
    const card=el('fieldset',{class:'card question',disabled:locked},
      el('div',{class:'question-head'},el('h2',{},`${index+1}. ${typeNames[question.type]}`),
        !locked?button('删除题目',()=>{survey.questions.splice(index,1);paint();},'danger'):null),
      field('题目',input(question.title,value=>question.title=value)),
      checkbox('必填',question.required,value=>question.required=value));
    if(['single','multiple'].includes(question.type)){
      question.options.forEach(option=>card.append(el('div',{class:'option-row'},
        el('span',{class:'help'},'选项'),
        input(option.label,value=>option.label=value,'text',{'aria-label':'选项文字'}),
        !locked?button('移除',()=>{question.options=question.options.filter(item=>item!==option);paint();},'danger'):null)));
      if(!locked)card.append(button('＋ 增加选项',()=>{question.options.push({id:identifier('o'),label:'新选项'});paint();}));
    }
    const constraints=el('div',{class:'grid'});
    const keys={multiple:[['minSelect','至少选择'],['maxSelect','至多选择']],
      text:[['minLength','最少字符数'],['maxLength','最多字符数']],
      number:[['min','最小数值'],['max','最大数值']]}[question.type]||[];
    for(const [key,label] of keys)constraints.append(field(label,
      input(question.validation[key],value=>{
        if(value==='')delete question.validation[key];
        else question.validation[key]=Number(value);
      },'number',{step:question.type==='number'?'any':'1',...(question.type!=='number'?{min:'0'}:{})})));
    if(question.type==='number')constraints.append(checkbox('只接受整数',question.validation.integer,
      value=>question.validation.integer=value));
    if(keys.length)card.append(el('details',{},el('summary',{},'填写限制'),
      el('p',{class:'help'},'留空表示不限制；服务器将在填写时检查。'),constraints));

    const operators={single:[['eq','等于选项']],multiple:[['contains','包含选项'],['contains_all','包含全部选项']],
      text:[],number:[['eq','等于'],['gt','大于'],['gte','大于等于'],['lt','小于'],['lte','小于等于']]}[question.type];
    const targets=[...survey.questions.slice(index+1).map((item,offset)=>
      [item.id,`${index+offset+2}. ${item.title||'未命名题目'}${offset===0?'（默认下一题）':''}`]),['END','结束问卷']];
    const rules=el('details',{},el('summary',{},`条件跳转（${question.rules.length} 条）`),
      el('p',{class:'help'},'按顺序匹配第一条符合的规则。若没有命中规则，自动进入下一题。跳到紧接着的下一题与默认行为相同；规则保存并发布后才会生效。'));
    question.rules.forEach((rule,ruleIndex)=>{
      const options=question.options.map(option=>[option.id,option.label||'(未命名选项)']);
      let valueInput;
      if(question.type==='single'||(question.type==='multiple'&&rule.operator==='contains')){
        const valid=options.some(([id])=>id===rule.value);
        const items=valid?options:[[String(rule.value),'失效选项，请重新选择'],...options];
        valueInput=select(items,String(rule.value??''),value=>rule.value=value);
      }else if(question.type==='multiple'&&rule.operator==='contains_all'){
        valueInput=el('div',{class:'stack'},question.options.map(option=>
          checkbox(option.label||'(未命名选项)',Array.isArray(rule.value)&&rule.value.includes(option.id),checked=>{
            const selected=Array.isArray(rule.value)?[...rule.value]:[];
            rule.value=checked?[...new Set([...selected,option.id])]:selected.filter(id=>id!==option.id);
          })));
      }else{
        valueInput=input(rule.value,value=>rule.value=value.trim()===''?null:Number(value),'number',{step:'any'});
      }
      const targetOptions=targets.some(([target])=>target===rule.target)
        ?targets:[[rule.target,'目标已删除，请重新选择'],...targets];
      rules.append(el('div',{class:'rule'},el('div',{class:'row'},
        field('如果答案',select(operators,rule.operator,value=>{
          rule.operator=value;
          if(question.type==='multiple'){
            rule.value=value==='contains_all'?[question.options[0]?.id].filter(Boolean):question.options[0]?.id||'';
          }
          paint();
        })),
        question.type==='multiple'&&rule.operator==='contains_all'
          ?el('div',{class:'field'},el('span',{},'条件值'),valueInput)
          :field('条件值',valueInput),
        field('跳转到',select(targetOptions,rule.target,value=>rule.target=value)),
        !locked?button('移除规则',()=>{question.rules.splice(ruleIndex,1);paint();},'danger'):null)));
    });
    if(!locked&&operators.length)rules.append(button('＋ 增加规则',()=>{
      const first=question.options[0]?.id||'';
      question.rules.push({operator:operators[0][0],value:question.type==='number'?0:first,target:'END'});
      paint();
    }));
    if(operators.length)card.append(rules);
    return card;
  }
  paint();
  return root;
}
async function fillView(id){
  if(!user)return el('section',{class:'card fill'},el('h1',{},'登录后填写问卷'),el('p',{},'所有填写者均须登录；允许匿名的问卷可在提交时选择隐藏身份。'),link('登录后继续','/login?return='+encodeURIComponent('/survey/'+id)));
  const survey=await api('/api/public/surveys/'+id);
  const root=el('div',{class:'fill'});
  if(survey.status!=='published'||(survey.deadline&&new Date(survey.deadline)<=new Date()))return el('section',{class:'card fill'},el('h1',{},survey.title),el('p',{},'问卷尚未开放或已结束，暂时无法填写。'));
  const answers=Object.create(null),path=[];let current=survey.questions[0]?.id || 'END',anonymous=false,submitting=false,navigating=false;
  function paint(){
    root.replaceChildren(el('h1',{class:'preview'},survey.title),el('p',{class:'muted preview'},survey.description));
    if(survey.deadline)root.append(el('p',{class:'help'},'截止：'+new Date(survey.deadline).toLocaleString()));
    const question=survey.questions.find(q=>q.id===current);
    if(current==='END'){
      const card=el('section',{class:'card'},el('h2',{},'已完成填写'),el('p',{class:'muted'},`已经过 ${path.length} 道题，请确认后提交。`));
      let anonymousInput=null;
      if(survey.allowAnonymous){
        const anonymousToggle=checkbox('匿名提交（不展示我的身份）',anonymous,value=>anonymous=value);
        anonymousInput=anonymousToggle.children[0];
        card.append(anonymousToggle);
        card.append(el('p',{class:'help'},'匿名提交不展示你的身份；系统仍按登录账号执行重复提交限制。'));
      }
      const backButton=path.length?button('上一步',previous):null;
      const submitButton=button('提交问卷',async()=>{
        if(submitting)return;
        submitting=true;
        if(backButton)backButton.disabled=true;
        if(anonymousInput)anonymousInput.disabled=true;
        try{
          await api('/api/public/surveys/'+id+'/responses','POST',{anonymous,answers});
          root.replaceChildren(el('section',{class:'card'},el('h1',{},'提交成功'),el('p',{},'回答已保存。'),link('返回首页','/')));
        }finally{
          submitting=false;
          if(backButton)backButton.disabled=false;
          if(anonymousInput)anonymousInput.disabled=false;
        }
      },'primary');
      card.append(el('div',{class:'actions'},backButton,submitButton));
      root.append(card);return;
    }
    if(!question){root.append(el('p',{class:'error'},'问卷题目不存在。'));return;}
    const position=survey.questions.findIndex(q=>q.id===current)+1;
    root.append(el('p',{class:'help'},`第 ${position} / ${survey.questions.length} 题 · ${typeNames[question.type]}${question.required?' · 必填':' · 选填'}`),el('div',{class:'progress','aria-hidden':'true'},el('span',{style:`width:${position/survey.questions.length*100}%`})));
    const card=el('section',{class:'card'},el('h2',{class:'preview'},question.title));
    let answer=answers[current] ?? null;
    if(question.type==='single'){
      for(const option of question.options)card.append(el('label',{class:'choice'},el('input',{type:'radio',name:question.id,value:option.id,checked:answer===option.id,onchange:()=>{answer=option.id;}}),option.label));
    }else if(question.type==='multiple'){
      answer=Array.isArray(answer)?[...answer]:[];
      for(const option of question.options)card.append(el('label',{class:'choice'},el('input',{type:'checkbox',value:option.id,checked:answer.includes(option.id),onchange:event=>{answer=event.target.checked?[...answer,option.id]:answer.filter(item=>item!==option.id);}}),option.label));
    }else if(question.type==='text')card.append(el('textarea',{value:answer??'','aria-label':question.title,oninput:event=>{answer=event.target.value;}}));
    else card.append(input(answer,value=>{answer=value===''?null:Number(value);},'number',{step:question.validation?.integer?'1':'any','aria-label':question.title}));
    const validation=question.validation||{},hints=[];
    for(const [key,label] of [['minSelect','至少选择'],['maxSelect','至多选择'],['minLength','最少字符数'],['maxLength','最多字符数'],['min','最小值'],['max','最大值']])if(validation[key]!==undefined)hints.push(label+' '+validation[key]);
    if(validation.integer)hints.push('只接受整数');
    if(hints.length)card.append(el('p',{class:'help'},hints.join(' · ')));
    card.append(el('div',{class:'actions'},path.length?button('上一步',previous):null,!question.required?button('跳过此题',()=>next(null)):null,button('下一步',()=>next(answer),'primary')));root.append(card);
  }
  async function next(answer){
    if(navigating||submitting)return;navigating=true;
    try {
    const result=await api('/api/public/surveys/'+id+'/next','POST',{questionId:current,answer});
    if(result.answer!==null&&result.answer!==undefined)answers[current]=result.answer;else delete answers[current];
    path.push(current);current=result.nextQuestionId;paint();
    } finally {navigating=false;}
  }
  function previous(){if(navigating||submitting)return;delete answers[current];current=path.pop();paint();}
  paint();return root;
}
async function statsView(id){
  const generation=routeGeneration,pending=new Set();
  const [survey,stats]=await Promise.all([api('/api/surveys/'+id),api('/api/surveys/'+id+'/stats?summaryOnly=true')]);
  const total=el('div',{class:'large-number',text:stats.totalResponses});
  const root=el('div',{},hero('回答统计',survey.title,link('← 问卷详情','/edit/'+id)),el('section',{class:'card'},el('p',{class:'muted'},'有效提交总数'),total,el('p',{class:'help'},'按提交记录计数，允许重复提交时同一人可能有多份回答。跳过或未提交的答案不计数；选填题允许留空时，已提交的空文本和空选项仍计入本题回答数。'),el('p',{class:'help'},'翻页会更新当前题目和提交总数，其他题目保留上次查询结果；刷新页面可更新全部题目。')));
  const list=el('div'),cards=new Map();
  function paintQuestion(question,card){
      card.replaceChildren(el('div',{class:'row between'},el('h2',{class:'preview'},question.title),el('span',{class:'badge'},typeNames[question.type])),el('p',{class:'help'},`${question.answered} 份回答`));
      if(['single','multiple'].includes(question.type))for(const option of question.options||[]){const percent=question.answered?Math.round(option.count/question.answered*100):0;card.append(el('div',{class:'stat-line'},el('div',{class:'row between'},el('span',{},option.label),el('span',{class:'muted'},`${option.count} 次选择 · ${percent}%`)),el('div',{class:'bar'},el('span',{style:`width:${percent}%`}))));}
      else{
        const offset=question.valuesOffset??0,limit=question.valuesLimit??20;
        if(question.type==='number')card.append(el('p',{},'平均值：'+(question.average===null||question.average===undefined?'暂无数据':Number(question.average.toFixed(4)))));
        const controls=[];
        if(limit===0){
          if(question.answered){
            card.append(el('p',{class:'help'},'填写内容按需加载。'));
            const load=button('查看填写内容',()=>loadPage(0));
            load.disabled=pending.has(question.id);
            controls.push(load);card.append(load);
          }else card.append(el('p',{class:'muted'},'暂无回答'));
        }else{
          card.append(el('ol',{class:'response-values',start:offset+1},(question.values||[]).map(value=>el('li',{},String(value)))));
          if(!question.values?.length)card.append(el('p',{class:'muted'},'暂无回答'));
          const previous=button('上一页',()=>loadPage(Math.max(0,offset-limit))),next=button('下一页',()=>loadPage(offset+limit));
          previous.disabled=pending.has(question.id)||offset===0;next.disabled=pending.has(question.id)||!question.hasMore;
          controls.push(previous,next);
          card.append(el('div',{class:'actions'},previous,el('span',{class:'help'},`第 ${Math.floor(offset/limit)+1} 页`),next));
        }
        async function loadPage(nextOffset){
          if(pending.has(question.id))return;
          pending.add(question.id);
          controls.forEach(control=>{control.disabled=true;});
          try{
            const params=new URLSearchParams({questionId:question.id,offset:nextOffset,limit:20});
            const page=await api('/api/surveys/'+id+'/stats?'+params);
            if(generation!==routeGeneration)return;
            const result=page.questions.find(item=>item.id===question.id);
            question.values=result.values;question.valuesOffset=result.valuesOffset;question.valuesLimit=result.valuesLimit;question.hasMore=result.hasMore;
            question.answered=result.answered;question.average=result.average;
            stats.totalResponses=page.totalResponses;total.textContent=stats.totalResponses;
          }catch(error){if(generation===routeGeneration)throw error;}
          finally{pending.delete(question.id);if(generation===routeGeneration&&cards.has(question.id))paintQuestion(question,cards.get(question.id));}
        }
      }
  }
  function paint(filter){
    list.replaceChildren();cards.clear();
    for(const question of stats.questions.filter(q=>!filter||q.id===filter)){
      const card=el('section',{class:'card'});cards.set(question.id,card);paintQuestion(question,card);
      list.append(card);
    }
  }
  root.append(el('section',{class:'card'},field('按题目查看',select([['','全部题目'],...stats.questions.map((q,i)=>[q.id,`${i+1}. ${q.title}`])],'',paint))),list);paint('');return root;
}
(async()=>{try{user=await api('/api/me');}catch{user=null;}await render();})();
