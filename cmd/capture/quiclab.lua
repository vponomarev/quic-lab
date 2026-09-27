-- QUIC Lab: decrypted application messages; transport dissection remains Wireshark's job.
local echo = Proto("qlab", "QUIC Lab Echo")
local f = {
 kind=ProtoField.string("qlab.kind", "Message type"),
 transport=ProtoField.string("qlab.transport", "Transport"),
 seq=ProtoField.uint64("qlab.seq", "Sequence", base.DEC),
 sent=ProtoField.uint64("qlab.sent_ns", "Client monotonic time (ns, not wall clock)", base.DEC),
 stream=ProtoField.uint64("qlab.stream_id", "Application stream ID", base.DEC),
 conn=ProtoField.string("qlab.connection_id", "Server connection ID"),
 peer=ProtoField.string("qlab.peer", "Client endpoint observed by server"),
 transit=ProtoField.bool("qlab.transit", "Transit probe"),
 supported=ProtoField.bool("qlab.transit_supported", "Transit supported"),
 ok=ProtoField.bool("qlab.transit_ok", "Transit probe succeeded"),
 raw=ProtoField.string("qlab.json", "JSON message"),
 note=ProtoField.string("qlab.note", "Note")
}
echo.fields=f
local json=Dissector.get("json")
local function u64(s)
 local lo,hi=0,0
 for d in s:gmatch("%d") do
  local n=lo*10+tonumber(d)
  hi=(hi*10+math.floor(n/4294967296))%4294967296
  lo=n%4294967296
 end
 return UInt64.new(lo,hi)
end
local function number(s,k) return s:match('"'..k..'"%s*:%s*(%d+)') end
local function stringval(s,k) return s:match('"'..k..'"%s*:%s*"([^"\\]*)"') end
local function boolval(s,k) return s:match('"'..k..'"%s*:%s*(%a+)') end
local function is_echo(s)
 return s:match('^%s*{') and s:match('}%s*$') and number(s,"seq") and number(s,"sent_ns") and number(s,"stream_id")
end
local function message(buf,pinfo,tree,transport)
 local s=buf:string()
 if not is_echo(s) then return false end
 local seq=number(s,"seq")
 local kind=stringval(s,"connection_id") and "Reply" or "Request"
 if boolval(s,"transit")=="true" then kind="Transit "..kind end
 local t=tree:add(echo,buf, "QUIC Lab Echo: "..kind..", seq="..seq)
 t:add(f.kind,kind);t:add(f.transport,transport)
 t:add(f.seq,u64(seq));t:add(f.sent,u64(number(s,"sent_ns")));t:add(f.stream,u64(number(s,"stream_id")))
 for key,field in pairs({connection_id=f.conn,peer=f.peer}) do
  local v=stringval(s,key);if v then t:add(field,v) end
 end
 for key,field in pairs({transit=f.transit,transit_supported=f.supported,transit_ok=f.ok}) do
  local v=boolval(s,key);if v=="true" or v=="false" then t:add(field,v=="true") end
 end
 t:add(f.raw,s)
 if json then json:call(buf:tvb(),pinfo,t) end
 pinfo.cols.protocol="QLAB Echo"
 pinfo.cols.info:append(" | "..kind.." seq="..seq)
 return true
end
function echo.dissector(tvb,pinfo,tree)
 local n=tvb:len();local offset=0
 while offset<n do
  local tail=tvb(offset):string();local ending=tail:find("\n",1,true)
  if not ending then
   if n-offset<65536 and pinfo.can_desegment>0 then
    pinfo.desegment_offset=offset;pinfo.desegment_len=DESEGMENT_ONE_MORE_SEGMENT
   else tree:add(echo,tvb(offset)):add(f.note,"Incomplete or oversized Echo message") end
   return n
  end
  if not message(tvb(offset,ending),pinfo,tree,"QUIC STREAM") then
   tree:add(echo,tvb(offset,ending)):add(f.note,"Unrecognized Echo JSON")
  end
  offset=offset+ending
 end
 return n
end
DissectorTable.get("quic.proto"):add("quic-lab/1",echo)
-- WebSocket text frames bypass ws heuristics in some Wireshark versions.
-- Inspect the decoded/unmasked text field after built-in WebSocket reassembly.
local ws=Proto("qlabws", "QUIC Lab WebSocket identification")
local wsText=Field.new("websocket.payload.text")
function ws.dissector(tvb,pinfo,tree)
 for _,field in ipairs({wsText()}) do
  local buf=field.range
  if buf and buf:len()<=65536 then message(buf,pinfo,tree,"HTTPS / WebSocket") end
 end
end
register_postdissector(ws)


local dg=Proto("qlabudp","QUIC Lab VPN UDP fragment")
local d={
 flow=ProtoField.uint64("qlabudp.flow_id","Flow ID (control stream ID)",base.DEC),
 seq=ProtoField.uint32("qlabudp.seq","UDP message sequence",base.DEC),
 index=ProtoField.uint16("qlabudp.fragment_index","Fragment index (zero based)",base.DEC),
 count=ProtoField.uint16("qlabudp.fragment_count","Fragment count",base.DEC),
 payload=ProtoField.bytes("qlabudp.payload","UDP payload fragment (no IP/UDP header)"),
 note=ProtoField.string("qlabudp.note","Note")
}
dg.fields=d
function dg.dissector(tvb,pinfo,tree)
 local n=tvb:len();local t=tree:add(dg,tvb())
 if n<16 or n>1016 then t:add(d.note,"Invalid fragment length");return n end
 t:add(d.flow,tvb(0,8));t:add(d.seq,tvb(8,4));t:add(d.index,tvb(12,2));t:add(d.count,tvb(14,2))
 t:add(d.payload,tvb(16))
 local index,count=tvb(12,2):uint(),tvb(14,2):uint()
 if count<1 or count>66 or index>=count then t:add(d.note,"Invalid fragment numbering") end
 pinfo.cols.protocol="QLAB UDP"
 pinfo.cols.info:append(" | UDP seq="..tvb(8,4):uint().." fragment="..(index+1).."/"..count)
 return n
end
DissectorTable.get("quic.proto.datagram"):add("quic-lab-gateway/1",dg)
