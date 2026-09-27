"""Offline dissector smoke test; requires tshark with Lua. No network traffic."""
import argparse, json, pathlib, struct, subprocess, tempfile
parser=argparse.ArgumentParser();parser.add_argument("tshark");args=parser.parse_args()
root=pathlib.Path(__file__).resolve().parent.parent
with tempfile.TemporaryDirectory() as tmp:
 p=pathlib.Path(tmp)
 (p/"harness.lua").write_text('DissectorTable.get("udp.port"):add(59000,Dissector.get("qlab"))\nDissectorTable.get("udp.port"):add(59001,Dissector.get("qlabudp"))\n')
 def packet(payload,port):
  udp=struct.pack("!HHHH",12345,port,8+len(payload),0)+payload
  return struct.pack("!BBHHHBBH4s4s",0x45,0,20+len(udp),0,0,64,17,0,b"\x7f\0\0\1",b"\x7f\0\0\1")+udp
 echo=json.dumps({"seq":18446744073709551615,"sent_ns":123456789,"stream_id":0}).encode()+b"\n"
 packets=[packet(echo*2,59000),packet(struct.pack("!QIHH",4,9,0,1)+b"hello",59001),packet(b"bad",59001)]
 with (p/"test.pcap").open("wb") as f:
  f.write(struct.pack("<IHHIIII",0xa1b2c3d4,2,4,0,0,65535,101))
  for i,b in enumerate(packets): f.write(struct.pack("<IIII",i,0,len(b),len(b))+b)
 cmd=[args.tshark,"-X","lua_script:"+str(root/"cmd/capture/quiclab.lua"),"-X","lua_script:"+str(p/"harness.lua"),"-r",str(p/"test.pcap"),"-T","fields","-e","qlab.seq","-e","qlabudp.flow_id","-e","qlabudp.seq","-e","qlabudp.note"]
 result=subprocess.run(cmd,text=True,capture_output=True,check=True)
 assert "Lua Error" not in result.stderr,result.stderr
 lines=result.stdout.splitlines()
 assert lines[0].startswith("18446744073709551615,18446744073709551615"),lines
 assert "4\t9" in lines[1],lines
 assert "Invalid fragment length" in lines[2],lines
 print("PASS: multiple Echo messages, exact uint64, UDP fragment header, truncated datagram")
