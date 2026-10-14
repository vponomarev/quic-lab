using System.IO;
using System.Net;
using System.Net.Http;
using System.Net.Http.Json;
using System.Security.Cryptography;
using System.Text;
using System.Text.Json;
using System.Text.RegularExpressions;
namespace QuicLab.Windows;

public sealed record UrlProfileBundle(IReadOnlyList<EngineSettings> Profiles);
public sealed class UrlProfileLoader {
 public const int MaxBytes=128*1024;
 readonly HttpClient http; readonly Func<string> installationId;
 public UrlProfileLoader(HttpClient http,Func<string>? installationId=null){this.http=http;this.installationId=installationId??ReadInstallationId;}
 public static HttpClient CreateHttpClient()=>new(new HttpClientHandler{AllowAutoRedirect=false,UseCookies=false}){Timeout=TimeSpan.FromSeconds(15)};
 static InvalidDataException Invalid()=>new("Неподдерживаемый или некорректный профиль. Нужен QUIC Lab JSON, AWG .conf или одна VLESS-ссылка.");
 public async Task<UrlProfileBundle> Load(string raw,CancellationToken ct){
  using var deadline=CancellationTokenSource.CreateLinkedTokenSource(ct);deadline.CancelAfter(TimeSpan.FromSeconds(15));ct=deadline.Token;
  raw=raw.Trim();if(raw.Length>8192||!Uri.TryCreate(raw,UriKind.Absolute,out var uri)||uri.Scheme!="https"||uri.Host.Length==0||uri.UserInfo.Length!=0)throw new InvalidDataException("Нужна HTTPS-ссылка без логина и пароля в адресе.");
  bool enrollment=uri.AbsolutePath.EndsWith("/enroll",StringComparison.Ordinal);
  if(enrollment){if(uri.AbsolutePath.EndsWith("/capture/enroll",StringComparison.Ordinal)||uri.Query.Length!=0||!Regex.IsMatch(uri.Fragment,"^#[0-9a-f]{64}$"))throw new InvalidDataException("Некорректная ссылка-приглашение VPN.");}
  else if(uri.Fragment.Length!=0)throw new InvalidDataException("Фрагмент ссылки допустим только для приглашения QUIC Lab.");
  ct.ThrowIfCancellationRequested();
  using var request=new HttpRequestMessage(enrollment?HttpMethod.Post:HttpMethod.Get,new UriBuilder(uri){Fragment=""}.Uri);
  if(enrollment){string id=Convert.ToHexString(SHA256.HashData(Encoding.UTF8.GetBytes(installationId()+"\n"+uri.AbsoluteUri))).ToLowerInvariant();request.Content=JsonContent.Create(new{token=uri.Fragment[1..],request_id=id,device_name="Windows "+Regex.Replace(Environment.MachineName,"[^a-zA-Z0-9 ._-]","_")[..Math.Min(Environment.MachineName.Length,80)]});}
  using var response=await http.SendAsync(request,HttpCompletionOption.ResponseHeadersRead,ct);
  if(response.StatusCode==HttpStatusCode.Gone&&enrollment)throw new InvalidDataException("Приглашение истекло, отозвано или исчерпало лимит устройств.");
  if(response.StatusCode!=HttpStatusCode.OK)throw new InvalidDataException($"Сервер не выдал профиль (HTTP {(int)response.StatusCode}). Используйте прямую ссылку; перенаправления не выполняются.");
  if(response.Content.Headers.ContentLength>MaxBytes)throw new InvalidDataException("Профиль больше 128 КиБ.");
  await using var input=await response.Content.ReadAsStreamAsync(ct);using var output=new MemoryStream();byte[] buffer=new byte[4096];int count;
  while((count=await input.ReadAsync(buffer,ct))!=0){if(output.Length+count>MaxBytes)throw new InvalidDataException("Профиль больше 128 КиБ.");await output.WriteAsync(buffer.AsMemory(0,count),ct);}
  return Parse(new UTF8Encoding(false,true).GetString(output.ToArray()));
 }
 public static UrlProfileBundle Parse(string raw){
  raw=raw.Trim().TrimStart('\uFEFF');if(Encoding.UTF8.GetByteCount(raw)>MaxBytes)throw Invalid();
  if(raw.StartsWith("vless://",StringComparison.Ordinal)&&!raw.Contains('\n')&&!raw.Contains('\r'))return new([new(){Transport="vless",Key=raw}]);
  if(raw.StartsWith("[Interface]",StringComparison.Ordinal))return new([new(){Transport="awg",Key=raw}]);
  try{
   using var doc=JsonDocument.Parse(raw,new JsonDocumentOptions{MaxDepth=16});var p=doc.RootElement;if(p.ValueKind!=JsonValueKind.Object)throw Invalid();
   string Get(string key)=>p.TryGetProperty(key,out var v)&&v.ValueKind==JsonValueKind.String?v.GetString()!:"";
   EngineSettings Shared(string kind)=>new(){Name=Get("name"),Transport=kind,Hostname=Get("hostname"),Certificate=Get("certificate"),Key=Get("key"),CA=Get("ca"),ServerName=Get("server_name"),VerifyName=Get("verify_name")};
   var profiles=new List<EngineSettings>();
   if(Get("kind")=="vpn"){
    int version=p.GetProperty("version").GetInt32();if(version is not (1 or 2))throw Invalid();
    string[] kinds=version==1?["quic","https"]:p.GetProperty("transports").EnumerateArray().Select(v=>v.GetString()??"").ToArray();
    if(kinds.Length is <1 or >4||kinds.Distinct().Count()!=kinds.Length)throw Invalid();
    foreach(string kind in kinds){var s=Shared(kind);switch(kind){case "quic":case "https":s.Endpoint=Get(kind);break;case "awg":s.Key=Get("awg_config");s.Certificate=s.Hostname=s.CA=s.ServerName=s.VerifyName="";break;case "vless":s.Key=Get("vless_uri");s.Certificate=s.Hostname=s.CA=s.ServerName=s.VerifyName="";break;default:throw Invalid();}profiles.Add(s);}
   }else if(Get("kind")==""&&Get("transport") is "quic" or "https" or "awg" or "vless") {var s=Shared(Get("transport"));s.Endpoint=Get("endpoint");profiles.Add(s);}else throw Invalid();
   foreach(var s in profiles){if(s.Key.Length==0||s.Name.Length>128)throw Invalid();if(s.Transport is "quic" or "https"){if(s.Hostname.Length==0||s.Certificate.Length==0||!Uri.TryCreate("https://"+s.Endpoint,UriKind.Absolute,out var endpoint)||endpoint.UserInfo.Length!=0||endpoint.AbsolutePath!="/"||endpoint.Query.Length!=0||endpoint.Fragment.Length!=0)throw Invalid();}}
   return new(profiles);
  }catch(Exception e)when(e is JsonException or InvalidOperationException or KeyNotFoundException or FormatException){throw Invalid();}
 }
 static string ReadInstallationId(){
  string folder=Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),"QUIC Lab","Windows");Directory.CreateDirectory(folder);
  // This random installation identifier is not a credential. Only its per-link hash leaves the machine.
  using var f=new FileStream(Path.Combine(folder,"enrollment-instance-id"),FileMode.OpenOrCreate,FileAccess.ReadWrite,FileShare.None);
  if(f.Length==0){byte[] id=Encoding.ASCII.GetBytes(Guid.NewGuid().ToString("D"));f.Write(id);f.Flush(true);return Encoding.ASCII.GetString(id);}
  if(f.Length!=36)throw new InvalidDataException("Повреждён локальный ID регистрации. Загрузка отменена.");byte[] bytes=new byte[36];f.ReadExactly(bytes);string value=Encoding.ASCII.GetString(bytes);if(!Guid.TryParseExact(value,"D",out _))throw new InvalidDataException("Некорректный локальный ID регистрации.");return value;
 }
}
