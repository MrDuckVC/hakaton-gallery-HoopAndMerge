import fs from 'node:fs';
import path from 'node:path';
import assert from 'node:assert/strict';
const root=path.resolve(import.meta.dirname,'..');
const read=file=>JSON.parse(fs.readFileSync(path.join(root,file),'utf8'));
const api=read('docs/api/openapi.json'), client=read('docs/api/ws-client.schema.json'),server=read('docs/api/ws-server.schema.json');
function resolve(ref,doc){assert(ref.startsWith('#/'),'external ref unsupported');return ref.slice(2).split('/').reduce((v,k)=>{assert(v&&Object.hasOwn(v,k),'Missing ref '+ref);return v[k];},doc);}
function refs(value,doc){if(!value||typeof value!=='object')return;if(value.$ref)resolve(value.$ref,doc);for(const v of Object.values(value))refs(v,doc);}
function check(v,s,doc){
 if(s.$ref)return check(v,resolve(s.$ref,doc),doc);
 if(s.anyOf){assert(s.anyOf.some(x=>{try{check(v,x,doc);return true;}catch{return false;}}),'anyOf mismatch');}
 if(s.oneOf){const matches=s.oneOf.filter(x=>{try{check(v,x,doc);return true;}catch{return false;}});assert.equal(matches.length,1,'oneOf mismatch');}
 if(Object.hasOwn(s,'const'))assert.deepEqual(v,s.const);
 if(s.enum)assert(s.enum.includes(v),'enum');
 if(s.type==='null')assert.equal(v,null);
 if(s.type==='object'){
  assert(v!==null&&!Array.isArray(v)&&typeof v==='object','object expected');
  for(const k of s.required??[])assert(Object.hasOwn(v,k),'missing '+k);
  for(const [k,x]of Object.entries(v)){if(s.properties?.[k])check(x,s.properties[k],doc);else if(s.additionalProperties===false)assert.fail('unknown '+k);else if(typeof s.additionalProperties==='object')check(x,s.additionalProperties,doc);}
 }
 if(s.type==='array'){assert(Array.isArray(v));for(const x of v)check(x,s.items,doc);}
 if(s.type==='string'){
  assert.equal(typeof v,'string');const len=Array.from(v).length;
  if(s.minLength!==undefined)assert(len>=s.minLength);if(s.maxLength!==undefined)assert(len<=s.maxLength);if(s.pattern)assert(new RegExp(s.pattern,'u').test(v),'pattern');
  if(s.format==='date-time')assert(!Number.isNaN(Date.parse(v))&&v.endsWith('Z'),'date-time');
 }
 if(s.type==='boolean')assert.equal(typeof v,'boolean');
 if(s.type==='number'||s.type==='integer'){
  assert(typeof v==='number'&&Number.isFinite(v));if(s.type==='integer')assert(Number.isSafeInteger(v));
  if(s.minimum!==undefined)assert(v>=s.minimum);if(s.maximum!==undefined)assert(v<=s.maximum);if(s.exclusiveMinimum!==undefined)assert(v>s.exclusiveMinimum);if(s.exclusiveMaximum!==undefined)assert(v<s.exclusiveMaximum);
 }
}
refs(api,api);refs(client,client);refs(server,server);
for (const name of ['RegisterRequest', 'LoginRequest']) {
 const schema=api.components.schemas[name];
 const credentials={username:'alice',password:' пароль '};
 if(name==='RegisterRequest')credentials.display_name='Алиса';
 check(credentials,schema,api);
 credentials.password='😀😀'; // Eight UTF-8 bytes, two code points.
 check(credentials,schema,api);
 credentials.username='\u0085ALICE\u2003';
 if(name==='RegisterRequest')credentials.display_name='  '+ '😀'.repeat(32)+'  ';
 check(credentials,schema,api); // Public text constraints apply after Unicode trim.
}
const paths=api.paths;assert(!paths['/guides/{guide_id}/messages']);assert(paths['/guides/{guide_id}/generations/by-request/{request_id}']);assert(paths['/photos/{photo_id}/thumbnail']);
for (const route of ['/auth/register','/auth/login']) for (const status of ['413','415']) assert(paths[route].post.responses[status],route+' missing '+status);
const serverTypes=server.oneOf.map(x=>x.properties.type.const);assert(!serverTypes.includes('world.rooms_available'));assert(serverTypes.includes('guide.cancelled'));
const operations=Object.values(paths).flatMap(x=>Object.values(x));assert.equal(new Set(operations.map(x=>x.operationId)).size,operations.length);
for(const operation of operations)for(const parameter of operation.parameters??[]){if(parameter.in==='path')assert.equal(parameter.required,true);}
const room=read('docs/api/examples/room.json');check(room,api.components.schemas.Room,api);
for(const name of ['world.snapshot','players.state','guide.completed'])check(read('docs/api/examples/'+name+'.json'),server,server);
const snapshot=read('docs/api/examples/world.snapshot.json');assert.equal(snapshot.payload.current_room.descriptor_bytes,fs.statSync(path.join(root,'docs/api/examples/room.json')).size);
const uuid='11111111-1111-4111-8111-111111111111';
check({type:'world.ready',request_id:uuid,payload:{world_version:'world_v1',movement_epoch:1}},client,client);
check({type:'player.move',payload:{movement_epoch:1,seq:1,position:{x:1,y:0,z:-1},rotation:{yaw:0,pitch:0}}},client,client);
assert.throws(()=>check({type:'move',payload:{}},client,client));
assert.throws(()=>check({type:'player.move',payload:{seq:1,position:{x:1,y:0,z:-1},rotation:{yaw:0,pitch:0}}},client,client));
const length=Array.from(fs.readFileSync(path.join(root,'AGENTS.md'),'utf8')).length;assert(length<=6000,'AGENTS length '+length);
// Проверка идентичности YAML и JSON-представлений используемому сериализатору.
function yaml(value,depth=0){const pad='  '.repeat(depth);if(Array.isArray(value))return value.length?value.map(v=>typeof v==='object'&&v!==null?pad+'-\n'+yaml(v,depth+1):pad+'- '+JSON.stringify(v)+'\n').join(''):pad+'[]\n';if(value&&typeof value==='object')return Object.keys(value).length?Object.entries(value).map(([k,v])=>pad+JSON.stringify(k)+':'+(v&&typeof v==='object'?'\n'+yaml(v,depth+1):' '+JSON.stringify(v)+'\n')).join(''):pad+'{}\n';return pad+JSON.stringify(value)+'\n';}
assert.equal(fs.readFileSync(path.join(root,'docs/api/openapi.yaml'),'utf8'),yaml(api));
console.log('PASS: references, operation IDs, YAML/JSON parity, four fixtures, ready/move, legacy rejection, room byte size, AGENTS '+length+'/6000 chars.');
console.log('This is a focused local check, not a full OpenAPI/JSON Schema conformance validator or backend runtime test.');
