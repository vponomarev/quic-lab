using System.Net;
using System.Text.Json;
using System.Text.Json.Serialization;
namespace QuicLab.Windows;
public sealed class EngineSettings {
 public string Name {get;set;}=""; public string Transport {get;set;}="quic"; public string Endpoint {get;set;}="";
 public string Hostname {get;set;}=""; public string Certificate {get;set;}=""; public string Key {get;set;}=""; public string CA {get;set;}="";
 [JsonPropertyName("server_name")] public string ServerName {get;set;}="";
 [JsonPropertyName("verify_name")] public string VerifyName {get;set;}="";
 [JsonPropertyName("interface_index")] public uint InterfaceIndex {get;set;}
 public string[] Targets {get;set;}=[];
}
public sealed class SettingsView {public ulong Revision {get;set;} public EngineSettings Settings {get;set;}=new(); [JsonPropertyName("has_key")]public bool HasKey {get;set;}}
public sealed class SettingsChange {
 [JsonPropertyName("expected_revision")]public ulong ExpectedRevision {get;set;}
 public EngineSettings Settings {get;set;}=new();
 [JsonPropertyName("replace_key")]public bool ReplaceKey {get;set;}
 public bool Reconnect {get;set;}
}
public record SettingsReply(Reply Status, SettingsView? View, string FieldError);
public interface ISettingsClient {Task<SettingsReply> SendSettingsAsync(string command,SettingsChange? change=null,CancellationToken cancellationToken=default);}
public static class SettingsDraft {
 public static readonly JsonSerializerOptions Json=new(){PropertyNamingPolicy=JsonNamingPolicy.CamelCase,PropertyNameCaseInsensitive=true};
 public static string Fingerprint(SettingsChange change)=>JsonSerializer.Serialize(change,Json);
 public static string? ValidateRoutes(string[] rows,string endpoint){
  if(rows.Length is <1 or >256)return "Добавьте от 1 до 256 подсетей.";
  var nets=new List<(uint Net,uint Mask)>();
  for(int i=0;i<rows.Length;i++){
   var parts=rows[i].Split('/');
   if(parts.Length!=2||!IPv4(parts[0],out var ip)||!int.TryParse(parts[1],out var bits)||bits is <1 or >32)return $"Строка {i+1}: требуется IPv4 CIDR, например 192.168.10.0/24.";
   uint mask=uint.MaxValue<<(32-bits);if((ip&mask)!=ip)return $"Строка {i+1}: укажите начало подсети.";
   if(IPv4(endpoint,out var host)&&(host&mask)==ip)return $"Строка {i+1}: подсеть содержит адрес VPN-сервера.";
   foreach(var (net,m) in nets)if((net&mask)==ip||(ip&m)==net)return $"Строка {i+1}: повтор или пересечение подсетей.";
   nets.Add((ip,mask));
  }
  return null;
 }
 static bool IPv4(string text,out uint value){value=0;var parts=text.Split('.');if(parts.Length!=4)return false;foreach(var part in parts){if(!byte.TryParse(part,out var b)||part!=b.ToString())return false;value=(value<<8)|b;}return true;}
}

public sealed class TransportImport {public string Name{get;set;}="";public string Endpoint{get;set;}="";[JsonPropertyName("suggested_targets")]public string[]? SuggestedTargets{get;set;}public string Notice{get;set;}="";}
public interface ITransportImportClient {Task<TransportImport> PreviewImport(string transport,string content,CancellationToken ct=default);}
