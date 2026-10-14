using QuicLab.Windows;
static void Check(bool value,string why){if(!value)throw new Exception(why);}
var draft = new ApplicationSelection();
draft.Add(@"C:\Games\Roblox.exe");
draft.Add(@"c:\games\ROBLOX.exe");
Check(draft.Current.Count==1,"duplicate path");
draft.Mode=ApplicationSelection.ExceptMode;
draft.Add(@"C:\Tools\Control.exe");
Check(draft.Current.Count==1,"independent exclusion list");
var copy=ApplicationSelection.Parse(draft.Serialize());
Check(copy.Mode==ApplicationSelection.ExceptMode&&copy.Current.Single()==@"C:\Tools\Control.exe","mode roundtrip");
copy.Mode=ApplicationSelection.OnlyMode;
Check(copy.Current.Single()==@"C:\Games\Roblox.exe","inactive list preserved");
copy.Remove(copy.Current.Single());Check(copy.Current.Count==0,"remove");
foreach(var path in new[]{"game.exe",@"\\host\share\game.exe",@"C:\game.exe:stream",@"C:\game.dll"}){
 try{draft.Add(path);throw new Exception("invalid path accepted");}catch(InvalidDataException){}
}
foreach(var raw in new[]{"{}","null","{\"version\":1,\"mode\":\"only_selected\",\"only\":[],\"only\":[],\"except\":[]}","{\"version\":1,\"mode\":\"only_selected\",\"only\":[],\"except\":[],\"extra\":true}"}){
 try{ApplicationSelection.Parse(raw);throw new Exception("invalid document accepted");}catch(InvalidDataException){}
}
var dir=Path.Combine(Path.GetTempPath(),"quic-app-draft-"+Guid.NewGuid());
try{
 var file=Path.Combine(dir,"draft.json");await draft.SaveAsync(file);
 var loaded=await ApplicationSelection.LoadAsync(file);Check(loaded.Current.Single()==@"C:\Tools\Control.exe","disk roundtrip");
 await File.WriteAllTextAsync(file,"broken");
 try{await ApplicationSelection.LoadAsync(file);throw new Exception("corruption hidden");}catch(InvalidDataException){}
 Check((await ApplicationSelection.LoadAsync(Path.Combine(dir,"missing.json"))).Current.Count==0,"new draft");
}finally{if(Directory.Exists(dir))Directory.Delete(dir,true);}
Console.WriteLine("Application selection: independent lists, strict import, persistence PASS");
