package admin

import (
 "encoding/json"
 "net/url"
 "strings"
 "testing"
)
func TestVLESSWebExportAndAuthorization(t *testing.T){
 s,_:=managedVLESSFixture(t);c:=config(t);c.DataDir=s.admissionDirectory();w:=NewWeb(c,s);h:=w.Handler()
 login:=call(h,"POST","/login",url.Values{"username":{c.Username},"password":{c.Password}}.Encode(),nil);cookie:=login.Result().Cookies()[0];csrf:=w.sessions[cookie.Value].CSRF
 u,e:=s.CreateWithProtocols("vless",[]string{"vless"});if e!=nil{t.Fatal(e)};form:=url.Values{"csrf":{csrf},"id":{u.ID}}
 exported:=call(h,"POST","/users/config",form.Encode(),cookie);var p Profile
 if exported.Code!=200||json.Unmarshal(exported.Body.Bytes(),&p)!=nil||p.VLESSURI==""||p.Key!=""{t.Fatal("managed VLESS export missing")}
 qr:=call(h,"POST","/users/vless-qr",form.Encode(),cookie);if qr.Code!=200||!strings.Contains(qr.Body.String(),"data:image/png;base64,"){t.Fatal("VLESS QR missing")}
  if strings.Contains(qr.Body.String(),"закрытый ключ AWG"){t.Fatal("VLESS QR claims to contain AWG private key")}
 users:=call(h,"GET","/users","",cookie)
 addForm:=strings.Split(strings.Split(users.Body.String(),"<form class=\"add-user\"")[1],"</form>")[0]
 if !strings.Contains(addForm,"value=\"vless\""){t.Fatal("VLESS unavailable in new-user form")}
 form.Del("csrf");if r:=call(h,"POST","/users/vless-qr",form.Encode(),cookie);r.Code!=403{t.Fatal("QR CSRF bypass")}
 if r:=call(h,"POST","/vless/config",form.Encode(),cookie);r.Code!=403{t.Fatal("settings CSRF bypass")}
 if r:=call(h,"GET","/vless","",nil);r.Code!=303{t.Fatal("settings authentication bypass")}
 page:=call(h,"GET","/vless","",cookie);if page.Code!=200||strings.Contains(page.Body.String(),s.state.VLESS.RealityPrivateKey){t.Fatal("settings leaked private key")}
}
