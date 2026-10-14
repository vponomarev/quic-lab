using System.Net;
using System.Net.Http;
using System.Text;
using System.Text.Json;
using QuicLab.Windows;
static class UrlImportAcceptance {
 const string Profile="""{"version":2,"kind":"vpn","name":"test","hostname":"vpn.example","transports":["quic","https"],"quic":"vpn.example:443","https":"vpn.example:8443","certificate":"cert","key":"secret","routes":"0.0.0.0/0"}""";
 public static async Task Run(){
  var h=new Handler();using var http=new HttpClient(h);var loader=new UrlProfileLoader(http,()=>"installation-one");
  h.Body=Profile;var bundle=await loader.Load("https://config.example/profile",CancellationToken.None);
  Check(bundle.Profiles.Count==2&&bundle.Profiles[0].Transport=="quic"&&bundle.Profiles[1].Endpoint=="vpn.example:8443","QUIC bundle");Check(bundle.Profiles[0].Targets.Length==0&&bundle.Profiles[0].InterfaceIndex==0,"remote routing imported");Check(h.Method==HttpMethod.Get,"direct GET");
  string link="https://config.example/lab/enroll#"+new string('a',64);
  await loader.Load(link,CancellationToken.None);string first=h.RequestBody;Check(h.Method==HttpMethod.Post&&!h.Url.Contains('#'),"token in URL or wrong method");using(var body=JsonDocument.Parse(first)){Check(body.RootElement.GetProperty("token").GetString()==new string('a',64),"token missing");}
  await new UrlProfileLoader(http,()=>"installation-one").Load(link,CancellationToken.None);Check(first==h.RequestBody,"retry changed registration ID");
  h.Body="vless://test";bundle=await loader.Load("https://config.example/vless",CancellationToken.None);Check(bundle.Profiles.Single().Key==h.Body,"VLESS raw");
  h.Body="[Interface]\nPrivateKey = test";bundle=await loader.Load("https://config.example/awg",CancellationToken.None);Check(bundle.Profiles.Single().Transport=="awg","AWG raw");
  h.Body="""{"transport":"https","endpoint":"vpn.example:443","hostname":"vpn.example","certificate":"cert","key":"secret","targets":["0.0.0.0/0"],"interface_index":88}""";bundle=await loader.Load("https://config.example/windows",CancellationToken.None);Check(bundle.Profiles.Single().Targets.Length==0&&bundle.Profiles[0].InterfaceIndex==0,"Windows route/interface preserved");
  foreach(var bad in new[]{"http://config.example/x","https://user:pass@config.example/x","https://config.example/x#token","https://config.example/capture/enroll#"+new string('a',64)})await Reject(()=>loader.Load(bad,CancellationToken.None));
  h.Status=HttpStatusCode.Found;await Reject(()=>loader.Load(link,CancellationToken.None));h.Status=HttpStatusCode.Gone;await Reject(()=>loader.Load(link,CancellationToken.None));h.Status=HttpStatusCode.OK;
  h.Body=new string('x',UrlProfileLoader.MaxBytes+1);await Reject(()=>loader.Load("https://config.example/x",CancellationToken.None));h.Body="<html>login</html>";await Reject(()=>loader.Load("https://config.example/x",CancellationToken.None));
  h.Body=Profile;using var cancel=new CancellationTokenSource();cancel.Cancel();await Reject(()=>loader.Load(link,cancel.Token));
  Check(UrlProfileLoader.Parse(Profile).Profiles.Count==2,"parse");Console.WriteLine("URL import: formats, HTTPS, registration retry, bounds, errors/cancellation and local routing PASS");
 }
 static void Check(bool ok,string why){if(!ok)throw new Exception(why);}
 static async Task Reject(Func<Task<UrlProfileBundle>> work){try{await work();}catch(Exception){return;}throw new Exception("unsafe/invalid import accepted");}
 sealed class Handler:HttpMessageHandler {public string Body="",Url="",RequestBody="";public HttpMethod? Method;public HttpStatusCode Status=HttpStatusCode.OK;protected override async Task<HttpResponseMessage> SendAsync(HttpRequestMessage q,CancellationToken ct){ct.ThrowIfCancellationRequested();Url=q.RequestUri!.AbsoluteUri;Method=q.Method;RequestBody=q.Content==null?"":await q.Content.ReadAsStringAsync(ct);return new(Status){Content=new StringContent(Body,Encoding.UTF8)};}}
}
