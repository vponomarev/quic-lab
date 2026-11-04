using System.Text.Json;
using System.Text.Json.Serialization;
namespace QuicLab.Windows;

public sealed class ProfileSummary {
 public string ID{get;set;}="";public string Name{get;set;}="";public string Transport{get;set;}="";public string Endpoint{get;set;}="";
 [JsonIgnore]public string Label=>$"{(string.IsNullOrWhiteSpace(Name)?"Без названия":Name)} · {Transport.ToUpperInvariant()} · {Endpoint}";
}
public sealed class ProfileList {public ulong Revision{get;set;}public ProfileSummary[] Profiles{get;set;}=[];}
public sealed class ProfileChange {
 [JsonPropertyName("replace_server_control")]public bool ReplaceServerControl{get;set;}
 public string ID{get;set;}="";
 [JsonPropertyName("expected_revision")]public ulong ExpectedRevision{get;set;}
 [JsonIgnore(Condition=JsonIgnoreCondition.WhenWritingNull)]public EngineSettings? Settings{get;set;}
 [JsonPropertyName("replace_key")]public bool ReplaceKey{get;set;}
 public bool Reconnect{get;set;}
}
public record ProfileLibraryReply(Reply Status,ProfileList? Library);
public interface IProfileLibraryClient {Task<ProfileLibraryReply> SendProfileAsync(string command,ProfileChange? change=null,CancellationToken cancellationToken=default);}
public sealed partial class PipeClient : IProfileLibraryClient {
 public async Task<ProfileLibraryReply> SendProfileAsync(string command,ProfileChange? change=null,CancellationToken cancellationToken=default){
  using var doc=await SendDocument(command,null,null,cancellationToken,profile:change);var root=doc.RootElement;
  return new(ParseStatus(root),root.TryGetProperty("profiles",out var library)?JsonSerializer.Deserialize<ProfileList>(library,SettingsDraft.Json):null);
 }
}
