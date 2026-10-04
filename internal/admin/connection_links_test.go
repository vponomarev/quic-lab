package admin

import (
 "html"
 "net/url"
 "regexp"
 "strings"
 "testing"
)
func TestConnectionLinksMatchQRPayloads(t *testing.T) {
 s,_:=managedVLESSFixture(t); c:=config(t); c.DataDir=s.admissionDirectory();w:=NewWeb(c,s);h:=w.Handler()
 login:=call(h,"POST","/login",url.Values{"username":{c.Username},"password":{c.Password}}.Encode(),nil);cookie:=login.Result().Cookies()[0];csrf:=w.sessions[cookie.Value].CSRF
 u,e:=s.CreateWithProtocols("links",[]string{"vless"});if e!=nil{t.Fatal(e)}
 form:=url.Values{"csrf":{csrf},"id":{u.ID}}
 for _,kind:=range []string{"qr","vless-qr"}{
  response:=call(h,"POST","/users/"+kind,form.Encode(),cookie)
  if response.Code!=200{t.Fatal(response.Code)}
  match:=regexp.MustCompile(`data-connection-link[^>]*>([^<]+)</textarea>`).FindStringSubmatch(response.Body.String())
  if len(match)!=2{t.Fatalf("%s: copyable link missing",kind)}
  raw:=html.UnescapeString(match[1])
  if kind=="vless-qr"{expected,e:=s.VLESSProfile(u.ID);if e!=nil||raw!=expected{t.Fatal("VLESS link differs from profile")}}else{
   prefix:=c.PublicURL+"enroll#";if !strings.HasPrefix(raw,prefix)||len(strings.TrimPrefix(raw,prefix))!=64{t.Fatal("invalid enrollment link")}
  }
  if response.Header().Get("Cache-Control")!="no-store"{t.Fatal("secret response cacheable")}
 }
}
