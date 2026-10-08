import readline from 'node:readline';
import {Readable} from 'node:stream';

// Public NOAA GHCNh coverage probe; read-only and separate from trading runtime.
const stations=[['KMIA','USW00012839'],['KAUS','USW00013904'],['EDDM','GMI0000EDDM']];
for(const [icao,id] of stations)for(const year of [2025,2026]) {
  const url=`https://www.ncei.noaa.gov/oa/global-historical-climatology-network/hourly/access/by-year/${year}/psv/GHCNh_${id}_${year}.psv`;
  try {
    const response=await fetch(url,{signal:AbortSignal.timeout(60000)});
    if(!response.ok){console.log(JSON.stringify({icao,year,status:response.status}));continue;}
    const reader=readline.createInterface({input:Readable.fromWeb(response.body),crlfDelay:Infinity});
    let index,rows=0,first='',last='';const types={},quality={},sources={};
    for await(const line of reader){
      if(!index){const h=line.split('|');index=Object.fromEntries(h.map((v,i)=>[v,i]));continue;}
      const cols=line.split('|');
      const stamp=cols[index.DATE],type=cols[index.temperature_Report_Type],q=cols[index.temperature_Quality_Code],source=cols[index.temperature_Source_Station_ID];
      if(!stamp)continue;
      rows++;if(!first)first=stamp;last=stamp;
      types[type]=(types[type]||0)+1;quality[q]=(quality[q]||0)+1;sources[source]=(sources[source]||0)+1;
    }
    console.log(JSON.stringify({icao,year,rows,first,last,types,quality,topSources:Object.entries(sources).sort((a,b)=>b[1]-a[1]).slice(0,5)}));
  }catch(e){console.log(JSON.stringify({icao,year,error:String(e)}));}
}
