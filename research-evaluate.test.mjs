import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtemp,writeFile,readFile,rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join,resolve} from 'node:path';
import {spawnSync} from 'node:child_process';
test('chronological temperature verification and availability rejection',async()=>{
 const dir=await mkdtemp(join(tmpdir(),'weather-verification-'));
 try{
  const rows=Array.from({length:40},(_,i)=>{const start=Date.UTC(2025,0,1+i*2),cutoff=start-24*3600000;return {station:'TEST',model:'fixture',local_date:new Date(start).toISOString().slice(0,10),lead_hours:24,forecast_c:20,observed_c:21,issued_at:new Date(cutoff-3600000).toISOString(),available_at:new Date(cutoff).toISOString(),cutoff_at:new Date(cutoff).toISOString(),target_start:new Date(start).toISOString(),target_end:new Date(start+86400000).toISOString(),observation_coverage:1,observation_source:'station_observation'}});
  const input=join(dir,'input.json'),output=join(dir,'output.json');await writeFile(input,JSON.stringify(rows));
  let result=spawnSync(process.execPath,[resolve('research-evaluate.mjs'),input,output],{encoding:'utf8'});assert.equal(result.status,0,result.stderr);
  const report=JSON.parse(await readFile(output,'utf8')).results[0];assert.equal(report.baseline_mae_c,1);assert.equal(report.corrected_mae_c,0);assert.equal(report.validation_dates,12);
  rows[0].available_at=rows[0].target_start;await writeFile(input,JSON.stringify(rows));result=spawnSync(process.execPath,[resolve('research-evaluate.mjs'),input,output],{encoding:'utf8'});assert.notEqual(result.status,0);
 }finally{await rm(dir,{recursive:true,force:true})}
});
