import fs from 'node:fs';
import path from 'node:path';
const root = path.resolve(import.meta.dirname, '..');
const write = (name, value) => { const p = path.join(root, name); fs.mkdirSync(path.dirname(p), {recursive:true}); fs.writeFileSync(p, value); };
const json = (name, value) => write(name, JSON.stringify(value,null,2)+'\n');
const ref = name => ({$ref:'#/components/schemas/'+name});
const str = extra => ({type:'string',...extra});
const integer = extra => ({type:'integer',...extra});
const number = extra => ({type:'number',...extra});
const arr = items => ({type:'array',items});
const obj = (properties, required=Object.keys(properties)) => ({type:'object',additionalProperties:false,properties,required});
const id = () => str({minLength:1,maxLength:128});
const date = () => str({format:'date-time'});
const nullable = schema => ({anyOf:[schema,{type:'null'}]});
const S = {};
S.Vector3 = obj({x:number(),y:number(),z:number()});
S.Rotation = obj({yaw:number(),pitch:number({minimum:-Math.PI/2,maximum:Math.PI/2})});
S.User = obj({id:id(),username:str(),display_name:str(),avatar_color:str({pattern:'^#[0-9A-Fa-f]{6}$'})});
S.AuthResponse = obj({user:ref('User')});
S.RegisterRequest = obj({username:str({pattern:'^[A-Za-z0-9_]{3,32}$'}),display_name:str({minLength:1,maxLength:32}),password:str({minLength:8,maxLength:128,writeOnly:true,description:'Дополнительно: не более 128 байт UTF-8; пароль не обрезается.'})});
S.LoginRequest = obj({username:str({minLength:1,maxLength:32}),password:str({minLength:1,maxLength:128,writeOnly:true})});
S.PlayerState = obj({room_id:id(),position:ref('Vector3'),rotation:ref('Rotation')});
S.MeResponse = obj({user:ref('User'),player_state:ref('PlayerState')});
S.ErrorResponse = obj({error:obj({code:str(),message:str(),request_id:id(),fields:{type:'object',additionalProperties:str()}},['code','message','request_id'])});
S.MovementConfig = obj({send_rate_hz:integer({minimum:1}),max_speed_mps:number({exclusiveMinimum:0}),player_radius_m:number({exclusiveMinimum:0}),camera_height_m:number({exclusiveMinimum:0})});
S.Manifest = obj({world_id:id(),world_version:id(),total_content_bytes:integer({minimum:1}),client_cache_limit_bytes:integer({minimum:1}),max_loaded_rooms:integer({minimum:1}),movement:ref('MovementConfig')});
S.AssetInfo = obj({id:id(),bytes:integer({minimum:1}),content_type:str({enum:['image/webp','image/jpeg','image/png','model/gltf-binary']})});
S.RoomAvailability = obj({room_id:id(),descriptor_bytes:integer({minimum:1}),assets:arr(ref('AssetInfo'))});
S.Bounds = obj({min:ref('Vector3'),max:ref('Vector3')});
S.Surface = obj({id:id(),position:ref('Vector3'),size:ref('Vector3'),color:str({pattern:'^#[0-9A-Fa-f]{6}$'}),collidable:{type:'boolean'}});
S.Exhibit = obj({id:id(),title:str(),description:str(),image_asset_id:id(),position:ref('Vector3'),rotation_y:number(),width_m:number({exclusiveMinimum:0}),height_m:number({exclusiveMinimum:0})});
S.Guide = obj({id:id(),display_name:str(),subtitle:str(),position:ref('Vector3'),rotation_y:number(),interaction_radius_m:number({exclusiveMinimum:0})});
S.Room = obj({id:id(),world_version:id(),title:str(),bounds:ref('Bounds'),surfaces:arr(ref('Surface')),exhibits:arr(ref('Exhibit')),guides:arr(ref('Guide'))});
S.DialogMessage = obj({id:id(),role:str({enum:['user','assistant']}),text:str(),created_at:date()});
S.DialogPage = obj({items:arr(ref('DialogMessage')),next_before:nullable(id())});
S.Photo = obj({id:id(),owner_id:id(),caption:str({maxLength:200}),room_id:id(),exhibit_id:nullable(id()),image_url:str(),image_bytes:integer({minimum:1}),created_at:date()});
S.PhotoPage = obj({items:arr(ref('Photo')),next_cursor:nullable(id())});
S.PhotoUpload = obj({image:{type:'string',format:'binary'},caption:str({maxLength:200}),room_id:id(),exhibit_id:id()},['image','room_id']);
S.PublicPlayer = obj({user_id:id(),display_name:str(),avatar_color:str(),room_id:id(),position:ref('Vector3'),rotation:ref('Rotation')});
S.PlayerMotion = obj({user_id:id(),ack_seq:integer({minimum:0}),room_id:id(),position:ref('Vector3'),rotation:ref('Rotation')});
const success = (schema,description='Успешно') => ({description,content:{'application/json':{schema}}});
const err = code => ({description:code,content:{'application/json':{schema:ref('ErrorResponse')}}});
const errors = codes => Object.fromEntries(codes.map(c=>[c,err(String(c))]));
const body = schema => ({required:true,content:{'application/json':{schema}}});
const param = (name,location,schema,required=true) => ({name,in:location,required,schema});
const operation = (operationId,summary,responses,extra={}) => ({operationId,summary,responses,...extra});
const P = {};
P['/auth/register']={post:operation('register','Регистрация и создание сессии',{'201':{...success(ref('AuthResponse')),headers:{'Set-Cookie':{schema:str(),description:'museum_session; HttpOnly; SameSite=Lax; Path=/; Max-Age=86400; Secure при HTTPS'}}},...errors([400,403,409,413,415,429])},{security:[],requestBody:body(ref('RegisterRequest'))})};
P['/auth/login']={post:operation('login','Вход и создание сессии',{'200':{...success(ref('AuthResponse')),headers:{'Set-Cookie':{schema:str()}}},...errors([400,401,403,413,415,429])},{security:[],requestBody:body(ref('LoginRequest'))})};
P['/auth/logout']={post:operation('logout','Отзыв текущей сессии',{'204':{description:'Cookie удалена; повторный выход также успешен'},...errors([403])},{security:[]})};
P['/me']={get:operation('getMe','Пользователь и сохранённая позиция',{'200':success(ref('MeResponse')),...errors([401])})};
P['/world/manifest']={get:operation('getManifest','Версия мира и бюджет контента',{'200':success(ref('Manifest')),...errors([401])})};
P['/world/rooms/{room_id}']={get:operation('getRoom','Точное неизменяемое описание доступного зала',{'200':success(ref('Room')),...errors([400,401,403,404,409])},{parameters:[param('room_id','path',id()),param('version','query',id())]})};
const binary = description => ({description,content:{'application/octet-stream':{schema:{type:'string',format:'binary'}}}});
P['/world/assets/{asset_id}']={get:operation('getAsset','Файл ресурса доступного зала',{'200':{description:'Ресурс; Content-Type соответствует AssetInfo',content:Object.fromEntries(['image/webp','image/jpeg','image/png','model/gltf-binary'].map(t=>[t,{schema:{type:'string',format:'binary'}}]))},...errors([400,401,403,404,409])},{parameters:[param('asset_id','path',id()),param('version','query',id())]})};
P['/guides/{guide_id}/messages']={get:operation('getGuideMessages','Личная сохранённая история диалога',{'200':success(ref('DialogPage')),...errors([400,401,404])},{parameters:[param('guide_id','path',id()),param('limit','query',integer({minimum:1,maximum:50,default:20}),false),param('before','query',id(),false)]})};
P['/photos']={post:operation('uploadPhoto','Загрузка снимка; требуется активное игровое соединение',{'201':success(ref('Photo')),...errors([400,401,403,409,413,415,429])},{requestBody:{required:true,content:{'multipart/form-data':{schema:ref('PhotoUpload')}}}}),get:operation('getPhotos','Собственный альбом, новые снимки первыми',{'200':success(ref('PhotoPage')),...errors([400,401])},{parameters:[param('limit','query',integer({minimum:1,maximum:50,default:20}),false),param('cursor','query',id(),false)]})};
P['/photos/{photo_id}/image']={get:operation('getPhotoImage','Изображение владельцу или авторизованному игроку после публикации',{'200':{description:'JPEG или WebP',content:{'image/jpeg':{schema:{type:'string',format:'binary'}},'image/webp':{schema:{type:'string',format:'binary'}}}},...errors([401,403,404])},{parameters:[param('photo_id','path',id())]})};
P['/ws']={get:operation('connectWebSocket','WebSocket upgrade; протокол в websocket.md',{'101':{description:'Switching Protocols'},...errors([400,401,403,429])},{description:'Первое событие world.snapshot. Это upgrade endpoint; JSON requestBody отсутствует.'})};
const api={openapi:'3.1.0',info:{title:'Museum Backend API',version:'1.0.0',description:'Контракт отдельного backend-репозитория. Нормативные правила: conventions.md и websocket.md. Фото — этап второго дня.'},servers:[{url:'/api/v1'}],security:[{sessionCookie:[]}],paths:P,components:{securitySchemes:{sessionCookie:{type:'apiKey',in:'cookie',name:'museum_session'}},schemas:S}};
function yaml(value, depth=0) {
 const pad='  '.repeat(depth);
 if(Array.isArray(value)) return value.length ? value.map(v=>typeof v==='object'&&v!==null ? pad+'-\n'+yaml(v,depth+1) : pad+'- '+JSON.stringify(v)+'\n').join('') : pad+'[]\n';
 if(value&&typeof value==='object') return Object.keys(value).length ? Object.entries(value).map(([k,v])=>pad+JSON.stringify(k)+':'+(v&&typeof v==='object' ? '\n'+yaml(v,depth+1) : ' '+JSON.stringify(v)+'\n')).join('') : pad+'{}\n';
 return pad+JSON.stringify(value)+'\n';
}
write('docs/api/openapi.yaml',yaml(api));
const pos={x:2,y:0,z:-3}, rot={yaw:0,pitch:0};
const room={id:'room_egypt_01',world_version:'world_v1',title:'Древний Египет',bounds:{min:{x:0,y:0,z:-12},max:{x:12,y:4,z:0}},surfaces:[{id:'floor_01',position:{x:6,y:-0.1,z:-6},size:{x:12,y:0.2,z:12},color:'#DCCFB8',collidable:true},{id:'wall_01',position:{x:6,y:2,z:-12},size:{x:12,y:4,z:0.2},color:'#E8DFCF',collidable:true}],exhibits:[{id:'exhibit_01',title:'Египетский рельеф',description:'Учебный экспонат о правилах изображения человеческой фигуры.',image_asset_id:'asset_egypt_01',position:{x:6,y:1.8,z:-11.8},rotation_y:0,width_m:1.5,height_m:1}],guides:[{id:'guide_herodotus',display_name:'Геродот',subtitle:'Художественная реконструкция историка и путешественника',position:{x:3,y:0,z:-6},rotation_y:0,interaction_radius_m:3}]};
json('docs/api/examples/room.json',room);
// descriptor_bytes — реальная длина файла fixture в UTF-8; размеры мира и картинки демонстрационные.
const descriptorBytes=fs.statSync(path.join(root,'docs/api/examples/room.json')).size;
const envelope=(type,payload,request_id)=>({type,event_id:'evt_01',...(request_id?{request_id}:{}),server_time:'2026-10-31T12:00:00Z',payload});
json('docs/api/examples/world.snapshot.json',envelope('world.snapshot',{protocol_version:1,world_version:'world_v1',self:{user_id:'usr_01',room_id:room.id,position:pos,rotation:rot},players:[{user_id:'usr_02',display_name:'Анна',avatar_color:'#E8A34B',room_id:room.id,position:{x:5,y:0,z:-4},rotation:rot}],available_rooms:[{room_id:room.id,descriptor_bytes:descriptorBytes,assets:[{id:'asset_egypt_01',bytes:180000,content_type:'image/webp'}]}]}));
json('docs/api/examples/players.state.json',envelope('players.state',{players:[{user_id:'usr_01',ack_seq:42,room_id:room.id,position:{x:2.4,y:0,z:-3},rotation:{yaw:0.3,pitch:0}},{user_id:'usr_02',ack_seq:0,room_id:room.id,position:{x:5,y:0,z:-4},rotation:rot}]}));
json('docs/api/examples/guide.completed.json',envelope('guide.completed',{generation_id:'gen_01',message:{id:'dialog_02',role:'assistant',text:'Ты уже осмотрел рельеф. Обрати внимание: разные части тела показаны с наиболее узнаваемых сторон. Это помогает прочитать изображение, а не воспроизводит один случайный взгляд.',created_at:'2026-10-31T12:00:00Z'},related_exhibit_ids:['exhibit_01']},'req_21'));
// JSON Schema событий — дополнительные машинно-проверяемые контракты.
const chatPhoto=obj({...S.Photo.properties},Object.keys(S.Photo.properties));
const client={
 'player.move':obj({seq:integer({minimum:1,maximum:9007199254740991}),position:ref('Vector3'),rotation:ref('Rotation')}),
 'chat.send':{...obj({text:str({maxLength:500}),photo_id:id()},[]),anyOf:[{required:['text'],properties:{text:str({minLength:1,maxLength:500})}},{required:['photo_id']}]},
 'exhibit.inspect':obj({exhibit_id:id()}),
 'guide.ask':obj({guide_id:id(),text:str({minLength:1,maxLength:2000}),exhibit_id:id()},['guide_id','text']),
 'guide.cancel':obj({generation_id:id()})
};
const server={
 'world.snapshot':obj({protocol_version:{const:1},world_version:id(),self:obj({user_id:id(),...S.PlayerState.properties}),players:arr(ref('PublicPlayer')),available_rooms:arr(ref('RoomAvailability'))}),
 'world.rooms_available':obj({current_room_id:id(),available_rooms:arr(ref('RoomAvailability'))}),
 'player.joined':ref('PublicPlayer'),
 'player.left':obj({user_id:id(),reason:str({enum:['disconnected','replaced','session_expired','logged_out','slow_consumer']})}),
 'players.state':obj({players:arr(ref('PlayerMotion'))}),
 'chat.message':obj({message_id:id(),author:obj({user_id:id(),display_name:str()}),text:str({maxLength:500}),photo:nullable(chatPhoto)},['message_id','author','text','photo']),
 'exhibit.inspected':obj({exhibit_id:id(),first_inspection:{type:'boolean'}}),
 'guide.accepted':obj({generation_id:id(),guide_id:id(),user_message_id:id(),queue_position:integer({minimum:0})}),
 'guide.delta':obj({generation_id:id(),index:integer({minimum:0}),text:str()}),
 'guide.completed':obj({generation_id:id(),message:{...ref('DialogMessage')},related_exhibit_ids:arr(id())}),
 'guide.failed':obj({generation_id:id(),code:str({enum:['AI_UNAVAILABLE','AI_TIMEOUT','CANCELLED','INTERNAL_ERROR']}),message:str(),retryable:{type:'boolean'}}),
 'error':obj({code:str(),message:str(),retryable:{type:'boolean'}})
};
function eventSchema(events,isClient){
 const variants=Object.entries(events).map(([type,payload])=>obj({type:{const:type},...(isClient?{...(type==='player.move'?{}:{request_id:id()})}:{event_id:id(),server_time:date(),request_id:id()}),payload},isClient?(type==='player.move'?['type','payload']:['type','request_id','payload']):['type','event_id','server_time','payload']));
 // Перенос ссылок компонентов в локальные $defs.
 return JSON.parse(JSON.stringify({$schema:'https://json-schema.org/draft/2020-12/schema',oneOf:variants,$defs:S}).replaceAll('#/components/schemas/','#/$defs/'));
}
json('docs/api/ws-client.schema.json',eventSchema(client,true));
json('docs/api/ws-server.schema.json',eventSchema(server,false));
write('docs/api/openapi.json',JSON.stringify(api,null,2)+'\n');
console.log('Generated OpenAPI YAML/JSON, WS schemas and examples.');
// Нормативная редакция пользователя: docs/api/contract.md. Переопределяет ранний эскиз выше.
const asciiID=()=>str({minLength:1,maxLength:128,pattern:'^[\\x21-\\x7E]+$'});
const uuid=()=>str({format:'uuid',pattern:'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-4[0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$'});
const safeInt=extra=>integer({minimum:0,maximum:9007199254740991,...extra});
// JSON Schema minLength считает code points, пароль ограничивается байтами UTF-8.
for (const name of ['RegisterRequest','LoginRequest']) {
 S[name].properties.username=str({description:'После Unicode White_Space trim: [A-Za-z0-9_]{3,32}; хранится и возвращается lowercase. Проверяет сервер.'});
}
S.RegisterRequest.properties.display_name=str({description:'1–32 Unicode-кодовые точки после Unicode White_Space trim. Проверяет сервер.'});
S.RegisterRequest.properties.password.minLength=1;
S.RegisterRequest.properties.password.description='8–128 байт UTF-8; без trim/нормализации. Байтовый минимум проверяет сервер.';
S.LoginRequest.properties.password.description='8–128 байт UTF-8; без trim/нормализации.';
S.Rotation.properties.yaw={...number(),minimum:-Math.PI,exclusiveMaximum:Math.PI};
S.PlayerState.properties.position={...ref('Vector3'),description:'Y=0; точка пола'};
S.Player=obj({user_id:asciiID(),display_name:str(),avatar_color:str({pattern:'^#[0-9A-Fa-f]{6}$'}),...S.PlayerState.properties,movement_epoch:safeInt({minimum:1}),ack_seq:safeInt()});
S.ErrorBody=obj({code:str(),message:str(),fields:{type:'object',additionalProperties:str()},retry_after_ms:safeInt()},['code','message']);
S.ErrorResponse=obj({error:obj({...S.ErrorBody.properties,request_id:asciiID()},['code','message','request_id'])});
S.MovementConfig=obj(Object.fromEntries(Object.entries({send_rate_hz:10,state_rate_hz:10,max_speed_mps:4,player_radius_m:0.3,player_height_m:1.8,camera_height_m:1.65}).map(([k,v])=>[k,{const:v}])));
S.Manifest=obj({world_id:asciiID(),world_version:asciiID(),total_content_bytes:safeInt({minimum:1}),client_cache_limit_bytes:safeInt({minimum:1}),max_loaded_rooms:{const:1},photo_reserve_bytes:{const:3145728},movement:ref('MovementConfig')});
S.AssetInfo=obj({id:asciiID(),bytes:safeInt({minimum:1}),content_type:str({enum:['image/jpeg','image/webp']})});
S.RoomPackage=obj({room_id:asciiID(),descriptor_bytes:safeInt({minimum:1}),assets:arr(ref('AssetInfo'))});
S.Door=obj({id:asciiID(),target_room_id:asciiID(),label:str(),position:ref('Vector3'),interaction_radius_m:{const:1.5}});
S.Exhibit.properties.interaction_radius_m={const:3};S.Exhibit.required.push('interaction_radius_m');
S.Guide.properties.interaction_radius_m={const:3};
S.Room.properties.doors=arr(ref('Door')); S.Room.required.push('doors');
S.Generation=obj({id:asciiID(),request_id:uuid(),guide_id:asciiID(),exhibit_id:nullable(asciiID()),status:str({enum:['queued','running','completed','failed']}),user_message:ref('DialogMessage'),assistant_message:nullable(ref('DialogMessage')),error:nullable(obj({code:str({enum:['AI_TIMEOUT','AI_UNAVAILABLE','SERVER_RESTARTED','CANCELLED','AI_OUTPUT_LIMIT']}),message:str(),retryable:{type:'boolean'}}))});
S.GenerationPage=obj({items:arr(ref('Generation')),next_before:nullable(asciiID())});
S.Generation.allOf=[
 {if:{properties:{status:{const:'completed'}}},then:{properties:{assistant_message:ref('DialogMessage'),error:{type:'null'}}}},
 {if:{properties:{status:{const:'failed'}}},then:{properties:{assistant_message:{type:'null'},error:obj({code:str({enum:['AI_TIMEOUT','AI_UNAVAILABLE','SERVER_RESTARTED','CANCELLED','AI_OUTPUT_LIMIT']}),message:str(),retryable:{type:'boolean'}})}}},
 {if:{properties:{status:{enum:['queued','running']}}},then:{properties:{assistant_message:{type:'null'},error:{type:'null'}}}}
];
Object.assign(S.Photo.properties,{published:{type:'boolean'},thumbnail_url:str(),thumbnail_bytes:safeInt({minimum:1,maximum:131072}),width:safeInt({minimum:1,maximum:1920}),height:safeInt({minimum:1,maximum:1080})}); S.Photo.required=Object.keys(S.Photo.properties);
S.PhotoUpload.properties.movement_epoch=str({pattern:'^[1-9][0-9]*$',description:'Десятичное целое <=2^53-1'});S.PhotoUpload.required.push('movement_epoch');
S.PhotoUpload.properties.image.description='Один неподвижный JPEG/WebP <=2097152 bytes; весь multipart <=2162688 bytes.';
for(const obsolete of ['PublicPlayer','PlayerMotion','RoomAvailability','DialogPage'])delete S[obsolete];
delete P['/guides/{guide_id}/messages'];
const genParams=[param('guide_id','path',asciiID())];
P['/guides/{guide_id}/generations']={get:operation('getGuideGenerations','Личная история генераций; внутри страницы от старых к новым',{'200':success(ref('GenerationPage')),...errors([400,401,404])},{parameters:[...genParams,param('limit','query',integer({minimum:1,maximum:50,default:20}),false),param('before','query',asciiID(),false)]})};
P['/guides/{guide_id}/generations/by-request/{request_id}']={get:operation('getGenerationByRequest','Восстановить вопрос по устойчивому request_id',{'200':success(ref('Generation')),...errors([400,401,404])},{parameters:[...genParams,param('request_id','path',uuid())]})};
P['/photos'].post.parameters=[param('Idempotency-Key','header',uuid())];
P['/photos'].post.summary='Идемпотентная загрузка фото из готовой игровой сессии';
P['/photos'].post.responses['200']=success(ref('Photo'),'Существующее фото при повторе того же ключа и содержимого');
P['/photos'].get.parameters[0].schema={type:'integer',minimum:1,maximum:6,default:6};
P['/photos/{photo_id}/thumbnail']={get:operation('getPhotoThumbnail','Миниатюра владельцу или после публикации',{'200':P['/photos/{photo_id}/image'].get.responses['200'],...errors([401,404])},{parameters:[param('photo_id','path',asciiID())]})};
delete P['/photos/{photo_id}/image'].get.responses['403'];
P['/world/assets/{asset_id}'].get.responses['200'].content=Object.fromEntries(['image/jpeg','image/webp'].map(t=>[t,{schema:{type:'string',format:'binary'}}]));
P['/world/rooms/{room_id}'].get.responses['409']=err('GAME_NOT_READY / WORLD_VERSION_MISMATCH');
P['/world/assets/{asset_id}'].get.responses['409']=err('GAME_NOT_READY / WORLD_VERSION_MISMATCH');
P['/ws'].get.responses['503']=err('SERVICE_UNAVAILABLE: максимум 32 игровых соединения');
for(const item of Object.values(P))for(const op of Object.values(item)){
 op.responses['500']=err('INTERNAL_ERROR');op.responses['503']??=err('SERVICE_UNAVAILABLE');
 for(const response of Object.values(op.responses)){
  response.headers={...response.headers,'Cache-Control':{schema:{const:'no-store'}},'X-Request-ID':{schema:asciiID()}};
 }
 if(op.responses['429']){
  op.responses['429'].headers['Retry-After']={schema:integer({minimum:1}),description:'ceil(retry_after_ms/1000)'};
 }
}
api.info.description='Отдельный Go backend. Нормативный источник docs/api/contract.md (редакция пользователя). Только один зал, двери, epoch, устойчивая идемпотентность гидов и фото. Все ответы no-store. Logout — явное исключение из требования сессии. Ограничения состояний и лимитов дополняют схемы.';
const epoch=safeInt({minimum:1});
const clientV1={
 'world.ready':obj({world_version:asciiID(),movement_epoch:epoch}),
 'world.enter':obj({door_id:asciiID(),movement_epoch:epoch}),
 'player.move':obj({movement_epoch:epoch,seq:safeInt({minimum:1}),position:ref('Vector3'),rotation:ref('Rotation')}),
 'chat.send':obj({text:str({maxLength:500}),photo_id:asciiID()},[]),
 'exhibit.inspect':obj({exhibit_id:asciiID()}),
 'guide.ask':obj({guide_id:asciiID(),text:str({minLength:1,maxLength:2000}),exhibit_id:asciiID()},['guide_id','text']),
 'guide.cancel':obj({generation_id:asciiID()})
};
const serverV1={
 'world.snapshot':obj({protocol_version:{const:1},world_version:asciiID(),self:ref('Player'),players:arr(ref('Player')),current_room:ref('RoomPackage')}),
 'world.ready':obj({movement_epoch:epoch}),
 'world.entered':obj({self:ref('Player'),current_room:ref('RoomPackage')}),
 'player.joined':obj({player:ref('Player')}),
 'player.left':obj({user_id:asciiID()}),
 'players.state':obj({players:arr(ref('Player'))}),
 'chat.message':obj({message_id:asciiID(),author:obj({user_id:asciiID(),display_name:str()}),text:str({maxLength:500}),photo:nullable(ref('Photo'))}),
 'exhibit.inspected':obj({exhibit_id:asciiID(),inspected_at:date()}),
 'guide.accepted':obj({generation:ref('Generation')}),
 'guide.started':obj({generation_id:asciiID()}),
 'guide.delta':obj({generation_id:asciiID(),index:safeInt(),text:str()}),
 'guide.completed':obj({generation:ref('Generation')}),
 'guide.failed':obj({generation:ref('Generation')}),
 'guide.cancelled':obj({generation:ref('Generation')}),
 'error':ref('ErrorBody')
};
function schemaV1(events,isClient){
 const variants=Object.entries(events).map(([type,payload])=>obj({type:{const:type},...(isClient?(type==='player.move'?{}:{request_id:uuid()}):{event_id:asciiID(),server_time:date(),request_id:uuid()}),payload},isClient?(type==='player.move'?['type','payload']:['type','request_id','payload']):['type','event_id','server_time','payload']));
 return JSON.parse(JSON.stringify({$schema:'https://json-schema.org/draft/2020-12/schema',description:'Структура событий. Условия состояний, trim, idempotency и корреляции см. contract.md.',oneOf:variants,$defs:S}).replaceAll('#/components/schemas/','#/$defs/'));
}
// Все ID в схемах ограничены ASCII, все integer безопасны для JS.
function normalize(v){if(!v||typeof v!=='object')return; if(v.type==='integer')v.maximum??=9007199254740991; if(v.type==='string'&&v.minLength===1&&v.maxLength===128&&!v.pattern&&!v.writeOnly)v.pattern='^[\\x21-\\x7E]+$'; for(const child of Object.values(v))normalize(child);}
normalize(api);normalize(clientV1);normalize(serverV1);
write('docs/api/openapi.yaml',yaml(api)); json('docs/api/openapi.json',api);
json('docs/api/ws-client.schema.json',schemaV1(clientV1,true));json('docs/api/ws-server.schema.json',schemaV1(serverV1,false));
room.doors=[{id:'door_egypt_01_02',target_room_id:'room_egypt_02',label:'Следующий зал',position:{x:10,y:0,z:-6},interaction_radius_m:1.5}];
room.exhibits[0].interaction_radius_m=3;json('docs/api/examples/room.json',room);
const roomPackage={room_id:room.id,descriptor_bytes:fs.statSync(path.join(root,'docs/api/examples/room.json')).size,assets:[{id:'asset_egypt_01',bytes:180000,content_type:'image/webp'}]};
const player={user_id:'usr_01',display_name:'Валентин',avatar_color:'#5B8DEF',room_id:room.id,position:pos,rotation:rot,movement_epoch:1,ack_seq:0};
const other={...player,user_id:'usr_02',display_name:'Анна',avatar_color:'#E8A34B',position:{x:5,y:0,z:-4}};
json('docs/api/examples/world.snapshot.json',envelope('world.snapshot',{protocol_version:1,world_version:'world_v1',self:player,players:[other],current_room:roomPackage}));
json('docs/api/examples/players.state.json',envelope('players.state',{players:[{...player,ack_seq:42,position:{x:2.4,y:0,z:-3},rotation:{yaw:0.3,pitch:0}},other]}));
const askID='11111111-1111-4111-8111-111111111111';
json('docs/api/examples/guide.completed.json',envelope('guide.completed',{generation:{id:'gen_01',request_id:askID,guide_id:'guide_herodotus',exhibit_id:'exhibit_01',status:'completed',user_message:{id:'dialog_01',role:'user',text:'Почему фигура изображена в профиль?',created_at:'2026-10-31T11:59:50Z'},assistant_message:{id:'dialog_02',role:'assistant',text:'Ты уже осмотрел рельеф. Части тела показаны с наиболее узнаваемых сторон: это помогает прочитать изображение.',created_at:'2026-10-31T12:00:00Z'},error:null}},askID));
console.log('Applied normative API v1 revision.');
