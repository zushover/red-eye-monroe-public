import {createSecureClient,relayerApiKey} from '@polymarket/client';
import {fetchBalanceAllowance} from '@polymarket/client/actions';
import {privateKey} from '@polymarket/client/viem';
import {AssetType} from '@polymarket/bindings/clob';
import {privateKeyToAccount} from 'viem/accounts';
import {writeFile} from 'node:fs/promises';

async function fetchWithRetry(url,options={}) {
 let lastError;
 for (let attempt=1;attempt<=3;attempt++) {
  try {
   return await fetch(url,{...options,signal:AbortSignal.timeout(15000)});
  } catch (error) {
   lastError=error;
   if (attempt<3) await new Promise(resolve=>setTimeout(resolve,500*attempt));
  }
 }
 throw lastError;
}

let wallet=process.env.POLYMARKET_WALLET_ADDRESS?.trim();
const rawSecret=process.env.SIGNER_PRIVATE_KEY?.trim();
const secret=/^[0-9a-fA-F]{64}$/.test(rawSecret||'')?`0x${rawSecret}`:rawSecret;
const relayerAddress=process.env.RELAYER_API_KEY_ADDRESS?.trim();
const relayerKey=process.env.RELAYER_API_KEY?.trim();
const invalid=[];
if (!/^0x[0-9a-fA-F]{40}$/.test(relayerAddress||'')) invalid.push('Relayer address');
if (!/^0x[0-9a-fA-F]{64}$/.test(secret||'')) invalid.push(`private key format (${rawSecret?.length||0} characters received; expected 64 hex characters, with optional 0x)`);
if (!relayerKey) invalid.push('Relayer API key');
if (invalid.length) {
 console.error(`Invalid or missing: ${invalid.join(', ')}. Never paste credentials into chat.`);
 process.exitCode=2;
} else {
 let stage='relayer credential check';
 try {
  if (!/^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/.test(relayerKey)) {
   const error=new Error('RELAYER_KEY_FORMAT');
   error.safeLength=relayerKey.length;
   throw error;
  }
  const signerAccount=privateKeyToAccount(secret);
  if (signerAccount.address.toLowerCase()!==relayerAddress.toLowerCase()) {
   throw new Error('SIGNER_MISMATCH');
  }
  console.log('Signer/private-key match: OK');
  const relayerResponse=await fetchWithRetry('https://relayer-v2.polymarket.com/relayer/api/keys',{headers:{RELAYER_API_KEY:relayerKey,RELAYER_API_KEY_ADDRESS:relayerAddress}});
  if (!relayerResponse.ok) {
   const error=new Error('RELAYER_AUTH_FAILED');
   error.safeStatus=relayerResponse.status;
   throw error;
  }
  console.log('Relayer API authentication: OK');
  if (!wallet) {
   stage='profile wallet discovery';
   const response=await fetchWithRetry(`https://gamma-api.polymarket.com/public-profile?address=${encodeURIComponent(relayerAddress)}`);
   if (!response.ok) throw new Error('PROFILE_LOOKUP_FAILED');
   const profile=await response.json();
   wallet=profile.proxyWallet;
  }
  if (!/^0x[0-9a-fA-F]{40}$/.test(wallet||'')) throw new Error('PROFILE_WALLET_NOT_FOUND');
  console.log('Profile wallet resolution: OK');
  // Authentication and reads only. No deploy/approval/transfer/order calls.
  stage='secure client authentication';
  const client=await createSecureClient({wallet,signer:privateKey(secret),apiKey:relayerApiKey({key:relayerKey,address:relayerAddress})});
  stage='balance read';
  const balance=await fetchBalanceAllowance(client,{assetType:AssetType.COLLATERAL});
  stage='open-order read';
  const orders=await client.listOpenOrders().firstPage();
  const report={checked_at:new Date().toISOString(),account:client.account,profile_wallet:wallet,balance,open_order_count:orders.items.length,live_enabled:false,status:'AUTHENTICATED_READ_ONLY',note:'Authentication only. No orders or approvals sent.'};
  await writeFile(new URL('../data/wallet-status.json',import.meta.url),JSON.stringify(report,(_,value)=>typeof value==='bigint'?value.toString():value,2),{mode:0o600});
  console.log('Authentication check completed. See data/wallet-status.json. No orders sent.');
 } catch (error) {
  const safeCode=typeof error?.cause?.code==='string'?`/${error.cause.code}`:'';
  const reason=error?.message==='RELAYER_KEY_FORMAT'?`Relayer API key format is invalid (received ${error.safeLength} characters; expected the 36-character UUID only).`:error?.message==='SIGNER_MISMATCH'?'The exported private key does not match the Relayer signer address.':error?.message==='RELAYER_AUTH_FAILED'?`Relayer credential check returned HTTP ${error.safeStatus}. Recreate or recopy the Relayer API key.`:error?.message==='PROFILE_WALLET_NOT_FOUND'?'Profile wallet was not found automatically; copy it from the Polymarket profile menu and retry.':`Wallet check failed during ${stage} (${error?.name||'Error'}${safeCode}). Verify the account fields and network locally.`;
  console.error(`${reason} No credential values were logged.`);
  process.exitCode=1;
 }
}
