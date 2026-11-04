using System.IO;
using System.Text.Json;
using System.Text.Json.Serialization;
using System.Text.RegularExpressions;
namespace QuicLab.Windows;

public sealed class ServerControlConfig {
 [JsonPropertyName("config_url")]public string ConfigURL{get;set;}="";
 [JsonPropertyName("device_id")]public string DeviceID{get;set;}="";
 [JsonPropertyName("update_token")]public string Token{get;set;}="";
 public ServerControlConfig Clone()=>new(){ConfigURL=ConfigURL,DeviceID=DeviceID,Token=Token};
 public bool Valid()=>Regex.IsMatch(DeviceID,"\\A[a-zA-Z0-9_-]{1,128}\\z")&&Regex.IsMatch(Token,"\\A[a-fA-F0-9]{64}\\z")&&
  ConfigURL.Length<=2048&&Uri.TryCreate(ConfigURL,UriKind.Absolute,out var u)&&u.Scheme=="https"&&u.Host.Length>0&&u.UserInfo.Length==0&&u.Query.Length==0&&u.Fragment.Length==0&&
  u.GetComponents(UriComponents.Path,UriFormat.UriEscaped)=="api/v1/devices/"+DeviceID+"/config"&&!ConfigURL.Contains('?')&&!ConfigURL.Contains('#')&&!ConfigURL.Contains('%');
}
public sealed class ServerExitChoice {
 public string ID{get;set;}="";public string Name{get;set;}="";public string Protocol{get;set;}="";public bool Disabled{get;set;}
 [JsonIgnore]public string Label=>ID.Length==0?"По умолчанию (назначает администратор)":$"{Name} ({ID})";
}
public sealed class ServerExitView {
 public string Selected{get;set;}="";public string Effective{get;set;}="";public string Revision{get;set;}="";
 public bool Legacy{get;set;}public bool Blocked{get;set;}public bool Pending{get;set;}
 public ServerExitChoice[] Choices{get;set;}=[];
}
public sealed class ServerExitChange {
 [JsonPropertyName("server_exit_id")]public string ID{get;set;}="";
 public string Revision{get;set;}="";
}
public sealed class ServerExitRequest {
 [JsonPropertyName("expected_revision")]public ulong ExpectedRevision{get;set;}
 [JsonIgnore(Condition=JsonIgnoreCondition.WhenWritingNull)]public ServerExitChange? Change{get;set;}
}
public record ServerExitReply(Reply Status,ServerExitView? View);
public interface IServerExitClient {Task<ServerExitReply> ServerExits(ServerExitRequest request,CancellationToken ct=default);}
public sealed partial class PipeClient : IServerExitClient {
 public async Task<ServerExitReply> ServerExits(ServerExitRequest request,CancellationToken ct=default){
  using var doc=await SendDocument(request.Change==null?"list_server_exits":"select_server_exit",null,null,ct,serverExit:request);
  var root=doc.RootElement;
  return new(ParseStatus(root),root.TryGetProperty("server_exit",out var view)?JsonSerializer.Deserialize<ServerExitView>(view,SettingsDraft.Json):null);
 }
}
