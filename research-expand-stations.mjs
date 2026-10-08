import {readFile,writeFile,mkdir} from 'node:fs/promises';
import {resolve} from 'node:path';
const root=import.meta.dirname,dir=resolve(root,'data/backtest/city-model-research');
await mkdir(dir,{recursive:true});
const latest=JSON.parse(await readFile(resolve(root,'data/latest.json'),'utf8'));
const old=JSON.parse(await readFile(resolve(root,'data/backtest/city-weather-calibration.json'),'utf8'));
const pilots=JSON.parse(await readFile(resolve(root,'configs/calibration-pilots.json'),'utf8'));
const csv=(await readFile(resolve(root,'data/backtest/isd-history.csv'),'utf8')).trim().split(/\r?\n/).map(line=>[...line.matchAll(/"([^"]*(?:""[^"]*)*)"(?:,|$)/g)].map(m=>m[1].replaceAll('""','"')));
const header=csv.shift(),records=csv.map(r=>Object.fromEntries(header.map((h,i)=>[h,r[i]])));
const map=new Map(pilots.map(p=>[p.icao,p])),missing=[];
function km(s,r){const lat=Number(r.LAT),lon=Number(r.LON),d=Math.PI/180,a=(lat-s.latitude)*d,b=(lon-s.longitude)*d;return 6371*2*Math.asin(Math.sqrt(Math.sin(a/2)**2+Math.cos(lat*d)*Math.cos(s.latitude*d)*Math.sin(b/2)**2));}
for(const report of latest.reports){const s=report.station;if(!s?.icao||map.has(s.icao))continue;const matches=records.filter(r=>r.ICAO===s.icao&&r.END>='20250820').map(r=>({...r,km:km(s,r)})).filter(r=>Number.isFinite(r.km)&&r.km<25).sort((a,b)=>a.km-b.km);if(!matches.length){missing.push({icao:s.icao,name:s.name,reason:'No active 2025 ICAO match within 25km in cached NOAA inventory'});continue;}const r=matches[0];map.set(s.icao,{icao:s.icao,name:s.name,latitude:s.latitude,longitude:s.longitude,timezone:s.timezone,isd_station:r.USAF+r.WBAN});}
await writeFile(resolve(dir,'pilots.json'),JSON.stringify([...map.values()],null,2));
await writeFile(resolve(dir,'inventory-gaps.json'),JSON.stringify([...new Map(missing.map(s=>[s.icao,s])).values()],null,2));
console.log(`Mapped ${map.size} stations; existing history ${old.stations.length}; inventory gaps ${new Set(missing.map(s=>s.icao)).size}`);
