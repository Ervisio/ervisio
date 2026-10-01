// Demo behaviour: "root" -> root disabled, password "errata" -> wrong credentials, anything else -> busy.
(function(){
  const q=document.querySelector('.q'); const f=q.querySelector('form');
  q.querySelectorAll('.eye').forEach(b=>b.addEventListener('click',()=>{const i=b.parentNode.querySelector('input');i.type=i.type==='password'?'text':'password'}));
  q.querySelectorAll('[data-user]').forEach(b=>b.addEventListener('click',()=>{const u=f.querySelector('[name=u]');u.value=b.dataset.user;q.classList.remove('err','root');q.querySelectorAll('[data-user]').forEach(x=>x.classList.toggle('on',x===b));const p=f.querySelector('[name=p]');p&&p.focus()}));
  f.addEventListener('submit',e=>{e.preventDefault();q.classList.remove('err','root','busy');
    const u=f.querySelector('[name=u]').value.trim(),p=f.querySelector('[name=p]').value;
    if(u==='root'){q.classList.add('root');return}
    if(!u||!p||p==='errata'){q.classList.add('err');return}
    q.classList.add('busy');setTimeout(()=>q.classList.remove('busy'),1800)});
})();
