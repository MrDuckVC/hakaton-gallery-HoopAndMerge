// Additive Sunday target contract; runtime implementation is tracked separately.
export function sundayContracts({S,P,client,server,obj,ref,str,number,integer,arr,nullable,id,uuid,date,write,json}) {
 const bounded=(schema,min,max)=>({...schema,minimum:min,maximum:max});
 const int=(min=0,max=9007199254740991)=>bounded(integer(),min,max);
 const num=(min,max)=>bounded(number(),min,max);
 const text=max=>str({minLength:1,maxLength:max,pattern:'^[^\\u0000]*$'});
 const bool={type:'boolean'}, color=str({pattern:'^#[0-9A-Fa-f]{6}$'});
 const list=(item,min,max)=>({...arr(item),minItems:min,maxItems:max});
 const e=values=>str({enum:values});
 const yaw={type:'number',minimum:-Math.PI,exclusiveMaximum:Math.PI};
 const named=(name,schema)=>{S['Sunday'+name]=schema;return ref('Sunday'+name);};
 const vec=named('Vec3',obj({x:number(),y:{const:0},z:number()}));
 const placement=named('GuidePlacement',obj({guide_id:id(),position:vec,rotation_y:yaw}));
 const slot=named('LampSlot',obj({slot_id:e(['a','b']),position:vec}));
 const config=named('RoomConfig',obj({accent_color:color,lighting:e(['neutral','warm','cool']),guide_placements:list(placement,0,3),lamp_slots:list(slot,2,2),npc_count:int(0,40),installation_visible:bool}));
 const guide=named('PublicGuide',obj({guide_id:id(),display_name:text(64),subtitle:text(200),position:vec,rotation_y:yaw}));
 const lamp=named('Lamp',obj({object_id:{const:'lamp_01'},slot_id:e(['a','b']),holder_user_id:nullable(id()),yaw,head_pitch:num(-Math.PI/3,Math.PI/3),on:bool}));
 const runtime=named('RoomRuntime',obj({world_version:id(),room_id:id(),revision:int(1),config,guides:list(guide,0,3),lamp}));
 const settings=named('GuideSettings',obj({display_name:text(64),subtitle:text(200),personality:text(2000),style:text(1000),topic:text(1000),constraints:list(text(1000),1,20),material_ids:{...list(id(),0,100),uniqueItems:true}}));
 const material=named('MaterialChoice',obj({id:id(),excerpt:text(200),exhibit_ids:list(id(),0,100)}));
 const adminGuide=named('AdminGuide',obj({world_version:id(),guide_id:id(),revision:int(1),settings,available_materials:list(material,0,100)}));
 const roomPut=named('RoomUpdate',obj({world_version:id(),expected_revision:int(1),config}));
 const guidePut=named('GuideUpdate',obj({world_version:id(),expected_revision:int(1),settings}));
 const adminMe=named('AdminMe',obj({user_id:id(),role:{const:'admin'}}));
 const metric=named('ClientMetric',obj({user_id:id(),display_name:str(),room_id:id(),fps:nullable(num(0,1000)),ping_ms:nullable(num(0,Number.MAX_SAFE_INTEGER)),visible:bool,sampled_at:nullable(date()),stale:bool}));
 const clients=named('Clients',obj({clients:list(metric,0,32)}));
 const rooms=named('Rooms',obj({rooms:arr(obj({room_id:id(),title:str(),revision:int(1)}))}));
 const guides=named('Guides',obj({guides:arr(obj({guide_id:id(),display_name:text(64),revision:int(1)}))}));
 const npc=named('NPC',obj({npc_id:str({pattern:'^npc_[0-3][0-9]$'}),position:vec,yaw,state:e(['walking','idle','talking']),speed_mps:num(0,.6),phase:{type:'number',minimum:0,exclusiveMaximum:2*Math.PI}}));
 const npcs=named('NPCState',obj({world_version:id(),room_id:id(),room_instance_id:id(),seq:int(1),npcs:list(npc,0,40)}));
 const commandBase={object_id:{const:'lamp_01'},movement_epoch:int(1),expected_revision:int(1)};
 const actions=named('ObjectAct',{oneOf:[
  obj({...commandBase,action:{const:'take'}}),
  obj({...commandBase,action:{const:'place'},slot_id:e(['a','b'])}),
  obj({...commandBase,action:{const:'aim'},yaw,head_pitch:num(-Math.PI/3,Math.PI/3)}),
  obj({...commandBase,action:{const:'power'},on:bool})]});
 const guideAction=named('GuideAction',obj({guide_id:id(),text:text(200),movement_epoch:int(1),expected_revision:int(1)}));
 const metrics=named('Metrics',{oneOf:[obj({fps:num(0,1000),visible:{const:true}}),obj({fps:{type:'null'},visible:{const:false}})]});
 const nonce=obj({nonce:str({minLength:16,maxLength:128,pattern:'^[A-Za-z0-9_-]+$'})});
 const effect=e(['show_installation','hide_installation','warm_light','cool_light']);
 Object.assign(client,{'room.sync':obj({}),'object.act':actions,'guide.action':guideAction,'client.metrics':metrics,'telemetry.pong':nonce});
 Object.assign(server,{'world.runtime':runtime,'npc.state':npcs,'object.acted':obj({action:e(['take','place','aim','power']),runtime}),'guide.action.completed':obj({guide_id:id(),effect,runtime}),'telemetry.probe':nonce});
 S.Manifest.properties.extensions={...arr({const:'sunday_v1'}),maxItems:1,uniqueItems:true};
 const reply=schema=>({description:'Успешно',content:{'application/json':{schema}}});
 const error={description:'Коды и условия — sunday.md',content:{'application/json':{schema:ref('ErrorResponse')}}};
 function endpoint(route,method,operationId,schema,input) {
  const responses={'200':reply(schema)};
  for(const status of [400,401,403,404,409,429,500,503])responses[status]=structuredClone(error);
  if(input)for(const status of [413,415])responses[status]=structuredClone(error);
  for(const response of Object.values(responses))response.headers={'Cache-Control':{schema:{const:'no-store'}},'X-Request-ID':{schema:id()}};
  responses['429'].headers['Retry-After']={schema:int(1)};
  const parameters=[...route.matchAll(/\{([^}]+)\}/g)].map(([,name])=>({name,in:'path',required:true,schema:id()}));
  const operation={operationId,summary:operationId,tags:['Sunday target (not implemented)'],description:'Целевой контракт, runtime пока не реализован. Только admin cookie; семантика/лимиты в sunday.md.',responses,parameters};
  if(input){operation.requestBody={required:true,content:{'application/json':{schema:input}}};operation.description+=' PUT: exact Origin, body <=65536 bytes, CAS expected_revision.';}
  P[route]??={};P[route][method]=operation;
 }
 endpoint('/admin/me','get','getAdminMe',adminMe);
 endpoint('/admin/clients','get','getAdminClients',clients);
 endpoint('/admin/rooms','get','getAdminRooms',rooms);
 endpoint('/admin/rooms/{room_id}','get','getAdminRoom',runtime);
 endpoint('/admin/rooms/{room_id}','put','updateAdminRoom',runtime,roomPut);
 endpoint('/admin/guides','get','getAdminGuides',guides);
 endpoint('/admin/guides/{guide_id}','get','getAdminGuide',adminGuide);
 endpoint('/admin/guides/{guide_id}','put','updateAdminGuide',adminGuide,guidePut);
 const exported=Object.fromEntries(Object.entries(S).filter(([name])=>name.startsWith('Sunday')));
 function ts(s) {
  if(s.$ref)return s.$ref.split('/').at(-1);
  if(Object.hasOwn(s,'const'))return JSON.stringify(s.const);
  if(s.enum)return s.enum.map(x=>JSON.stringify(x)).join(' | ');
  if(s.oneOf||s.anyOf)return (s.oneOf??s.anyOf).map(x=>'('+ts(x)+')').join(' | ');
  if(s.type==='object')return '{ '+Object.entries(s.properties).map(([k,v])=>k+(s.required.includes(k)?'':'?')+': '+ts(v)).join('; ')+' }';
  if(s.type==='array')return 'Array<'+ts(s.items)+'>';
  return {integer:'number',number:'number',string:'string',boolean:'boolean',null:'null'}[s.type];
 }
 let types='// GENERATED by tools/build-contracts.mjs. Target extension, not runtime support.\n// Copy into frontend and merge the event unions with the base contract.\n';
 for(const [name,schema]of Object.entries(exported))types+='export type '+name+' = '+ts(schema)+';\n';
 const special=new Set(['client.metrics','telemetry.pong']);
 const cTypes=['room.sync','object.act','guide.action','client.metrics','telemetry.pong'];
 const sTypes=['world.runtime','npc.state','object.acted','guide.action.completed','telemetry.probe'];
 types+='export type SundayClientEvent =\n'+cTypes.map(name=>'  { type: '+JSON.stringify(name)+(special.has(name)?'':'; request_id: string')+'; payload: '+ts(client[name])+' }').join(' |\n')+';\n';
 types+='export type SundayServerEvent =\n'+sTypes.map(name=>'  { type: '+JSON.stringify(name)+'; event_id: string; server_time: string; request_id?: string; payload: '+ts(server[name])+' }').join(' |\n')+';\n';
 write('docs/api/sunday.types.ts',types);
 const roomID='room_leonardo_portrait',guideID='guide_leonardo';
 const p={x:6,y:0,z:7.5};
 const runtimeExample={world_version:'gallery_c01_v1',room_id:roomID,revision:1,config:{accent_color:'#D9D2C3',lighting:'neutral',guide_placements:[{guide_id:guideID,position:p,rotation_y:0}],lamp_slots:[{slot_id:'a',position:{x:3,y:0,z:4}},{slot_id:'b',position:{x:9,y:0,z:4}}],npc_count:40,installation_visible:false},guides:[{guide_id:guideID,display_name:'Леонардо',subtitle:'Учебная реконструкция',position:p,rotation_y:0}],lamp:{object_id:'lamp_01',slot_id:'a',holder_user_id:null,yaw:0,head_pitch:0,on:true}};
 const requestID='11111111-1111-4111-8111-111111111111';
 const event=(type,payload,correlated=false)=>({type,event_id:'evt_sunday_'+type.replaceAll('.','_'),server_time:'2026-11-01T12:00:00Z',...(correlated?{request_id:requestID}:{}),payload});
 const command=(type,payload)=>({type,...(special.has(type)?{}:{request_id:requestID}),payload});
 const example=(name,value)=>json('docs/api/examples/sunday/'+name+'.json',value);
 example('world.runtime',event('world.runtime',runtimeExample));
 example('room.sync',command('room.sync',{}));
 const npcPayload={world_version:'gallery_c01_v1',room_id:roomID,room_instance_id:'room_instance_01',seq:1,npcs:Array.from({length:40},(_,i)=>({npc_id:'npc_'+String(i).padStart(2,'0'),position:{x:2+(i%8),y:0,z:3+Math.floor(i/8)*.7},yaw:0,state:'walking',speed_mps:.6,phase:(i*.5)%(2*Math.PI)}))};
 example('npc.state',event('npc.state',npcPayload));
 for(const [action,extra]of Object.entries({take:{},place:{slot_id:'b'},aim:{yaw:.4,head_pitch:.2},power:{on:false}}))example('object.act.'+action,command('object.act',{object_id:'lamp_01',movement_epoch:1,expected_revision:1,action,...extra}));
 example('object.acted',event('object.acted',{action:'take',runtime:{...runtimeExample,revision:2,lamp:{...runtimeExample.lamp,holder_user_id:'usr_01'}}},true));
 example('guide.action',command('guide.action',{guide_id:guideID,text:'покажи макет',movement_epoch:1,expected_revision:1}));
 example('guide.action.completed',event('guide.action.completed',{guide_id:guideID,effect:'show_installation',runtime:{...runtimeExample,revision:2,config:{...runtimeExample.config,installation_visible:true}}},true));
 example('client.metrics',command('client.metrics',{fps:58.5,visible:true}));
 example('client.metrics.hidden',command('client.metrics',{fps:null,visible:false}));
 for(const name of ['telemetry.probe','telemetry.pong'])example(name,name.endsWith('probe')?event(name,{nonce:'challenge_1234567890'}):command(name,{nonce:'challenge_1234567890'}));
 example('admin.clients',{clients:[{user_id:'usr_01',display_name:'Посетитель',room_id:roomID,fps:58.5,ping_ms:12.3,visible:true,sampled_at:'2026-11-01T12:00:00Z',stale:false}]});
 example('admin.room.update',{world_version:'gallery_c01_v1',expected_revision:1,config:{...runtimeExample.config,lighting:'warm'}});
 const settingsExample={display_name:'Леонардо',subtitle:'Учебная реконструкция',personality:'Учебная реконструкция Леонардо да Винчи.',style:'Объясняй простыми словами.',topic:'Искусство Возрождения.',constraints:['Не придумывай исторические цитаты.'],material_ids:[]};
 example('admin.guide',{world_version:'gallery_c01_v1',guide_id:guideID,revision:1,settings:settingsExample,available_materials:[]});
 example('admin.guide.update',{world_version:'gallery_c01_v1',expected_revision:1,settings:settingsExample});
 example('error.revision',event('error',{code:'REVISION_CONFLICT',message:'Состояние зала изменилось. Обновите его.'},true));
}
