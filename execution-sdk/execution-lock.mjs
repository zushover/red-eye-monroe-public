import {open as fsOpen,readFile,unlink} from 'node:fs/promises';
const alive=pid=>{try{process.kill(pid,0);return true;}catch(e){return e.code!=='ESRCH';}};
export async function executionLock(path){
 async function create(){const h=await fsOpen(path,'wx',0o600);await h.writeFile(JSON.stringify({pid:process.pid,created_at:new Date().toISOString()}));return h;}
 try{return await create();}catch(error){if(error.code!=='EEXIST')throw error;}
 const recovery=await fsOpen(path+'.recovery','wx',0o600);
 try{
  let owner;try{owner=JSON.parse(await readFile(path,'utf8'));}catch(error){if(error.code==='ENOENT')return await create();throw Error('Execution lock owner unavailable; manual inspection required');}
  if(!Number.isInteger(owner.pid)||owner.pid<=0||alive(owner.pid))throw Error('Execution transaction in progress; retry next scan');
  await unlink(path);return await create();
 }finally{await recovery.close();await unlink(path+'.recovery').catch(()=>{});}
}
