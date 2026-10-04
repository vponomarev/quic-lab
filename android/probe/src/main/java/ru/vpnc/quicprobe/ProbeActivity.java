package ru.vpnc.quicprobe;
import android.app.Activity;
import android.os.Bundle;
import android.widget.TextView;
import java.net.*;
import java.io.*;
import java.nio.charset.StandardCharsets;
import javax.net.ssl.*;
import org.json.JSONObject;

// Debug-only companion with its own UID. Contains no VPN credentials.
public class ProbeActivity extends Activity {
 private static byte[] readN(InputStream in,int limit)throws IOException{ByteArrayOutputStream out=new ByteArrayOutputStream();byte[] buf=new byte[limit];while(out.size()<limit){int n=in.read(buf,0,limit-out.size());if(n<0)break;out.write(buf,0,n);}return out.toByteArray();}
 @Override public void onCreate(Bundle b){super.onCreate(b);TextView text=new TextView(this);text.setText("TCP/UDP test probe");setContentView(text);
 final String host=getIntent().getStringExtra("host"),kind=getIntent().getStringExtra("kind"),id=getIntent().getStringExtra("id");final int port=getIntent().getIntExtra("port",39081);
 if(host==null||id==null||!id.matches("[a-zA-Z0-9_-]{1,64}")){finish();return;}
 if("soak".equals(kind)){android.content.Intent i=new android.content.Intent(this,SoakService.class);i.putExtras(getIntent());startForegroundService(i);return;}
 new Thread(()->{JSONObject result=new JSONObject();try{
 result.put("id",id).put("kind",kind).put("uid",android.os.Process.myUid());
 android.net.ConnectivityManager cm=getSystemService(android.net.ConnectivityManager.class);android.net.NetworkCapabilities caps=cm.getNetworkCapabilities(cm.getActiveNetwork());result.put("vpn",caps!=null&&caps.hasTransport(android.net.NetworkCapabilities.TRANSPORT_VPN));
 byte[] payload=("quic-lab-probe-"+id).getBytes(StandardCharsets.UTF_8);String reply;
 if("https-long".equals(kind)){try(SSLSocket s=(SSLSocket)SSLSocketFactory.getDefault().createSocket(host,port)){
 SSLParameters params=s.getSSLParameters();params.setEndpointIdentificationAlgorithm("HTTPS");s.setSSLParameters(params);s.setSoTimeout(10000);s.startHandshake();
 InputStream in=s.getInputStream();long end=android.os.SystemClock.elapsedRealtime()+45000,maxRtt=0;int count=0;
 while(android.os.SystemClock.elapsedRealtime()<end){long start=android.os.SystemClock.elapsedRealtime();
 s.getOutputStream().write(("GET /bond-connectivity-test HTTP/1.1\r\nHost: "+host+"\r\nConnection: keep-alive\r\n\r\n").getBytes(StandardCharsets.US_ASCII));
 ByteArrayOutputStream headers=new ByteArrayOutputStream();int state=0;
 while(state<4){int b0=in.read();if(b0<0)throw new EOFException("HTTP headers");headers.write(b0);if(headers.size()>16384)throw new IOException("headers too large");int expected=(state==0||state==2)?13:10;state=b0==expected?state+1:(b0==13?1:0);}
 String hs=headers.toString("US-ASCII");int length=-1;for(String line:hs.split("\r\n")){if(line.toLowerCase(java.util.Locale.ROOT).startsWith("content-length:"))length=Integer.parseInt(line.substring(15).trim());}
 if(length<0||length>65536)throw new IOException("Expected bounded Content-Length");if(readN(in,length).length!=length)throw new EOFException("HTTP body");
 maxRtt=Math.max(maxRtt,android.os.SystemClock.elapsedRealtime()-start);count++;Thread.sleep(100);}
 result.put("exchanges",count).put("max_rtt_ms",maxRtt).put("connections",1);reply=new String(payload,StandardCharsets.UTF_8);
 }}
 else if("tcp-long".equals(kind)){try(Socket s=new Socket()){
 s.connect(new InetSocketAddress(host,port),5000);s.setSoTimeout(10000);
 long end=android.os.SystemClock.elapsedRealtime()+45000,maxRtt=0;int count=0;
 while(android.os.SystemClock.elapsedRealtime()<end){long start=android.os.SystemClock.elapsedRealtime();s.getOutputStream().write(payload);byte[] response=readN(s.getInputStream(),payload.length);if(!java.util.Arrays.equals(payload,response))throw new IOException("TCP bytes differ");maxRtt=Math.max(maxRtt,android.os.SystemClock.elapsedRealtime()-start);count++;Thread.sleep(100);}
 result.put("exchanges",count).put("max_rtt_ms",maxRtt).put("connections",1);reply=new String(payload,StandardCharsets.UTF_8);
 }}
 else if("dns".equals(kind)){try(DatagramSocket s=new DatagramSocket()){
 byte[] query=new byte[]{0x41,0x62,1,0,0,1,0,0,0,0,0,0,7,101,120,97,109,112,108,101,3,99,111,109,0,0,1,0,1};
 s.setSoTimeout(10000);s.connect(InetAddress.getByName(host),port);s.send(new DatagramPacket(query,query.length));DatagramPacket answer=new DatagramPacket(new byte[4096],4096);s.receive(answer);byte[] data=answer.getData();
 if(answer.getLength()<12||data[0]!=query[0]||data[1]!=query[1]||(data[2]&0x80)==0||(data[3]&15)!=0)throw new IOException("Invalid DNS response");reply=new String(payload,StandardCharsets.UTF_8);
 }}
 else if("udp".equals(kind)){try(DatagramSocket s=new DatagramSocket()){s.setSoTimeout(5000);s.connect(InetAddress.getByName(host),port);s.send(new DatagramPacket(payload,payload.length));byte[] buf=new byte[1024];DatagramPacket p=new DatagramPacket(buf,buf.length);s.receive(p);reply=new String(buf,0,p.getLength(),StandardCharsets.UTF_8);}}
 else if("https".equals(kind)){HttpsURLConnection c=(HttpsURLConnection)new URL("https://"+host+"/").openConnection();c.setConnectTimeout(5000);c.setReadTimeout(5000);c.setRequestProperty("Connection","close");try(InputStream in=c.getInputStream()){reply=new String(readN(in,100),StandardCharsets.UTF_8);}finally{c.disconnect();}}
 else{try(Socket s=new Socket()){s.connect(new InetSocketAddress(host,port),5000);s.setSoTimeout(5000);s.getOutputStream().write(payload);byte[] buf=readN(s.getInputStream(),payload.length);reply=new String(buf,StandardCharsets.UTF_8);}}
 result.put("ok", "https".equals(kind)?!reply.trim().isEmpty():reply.equals(new String(payload,StandardCharsets.UTF_8))).put("reply",reply);
 }catch(Exception e){try{result.put("ok",false).put("error",e.getClass().getSimpleName()+": "+e.getMessage());}catch(Exception ignored){}}
 try(FileOutputStream out=openFileOutput(id+".json",MODE_PRIVATE)){out.write(result.toString().getBytes(StandardCharsets.UTF_8));}catch(Exception ignored){}
 runOnUiThread(()->text.setText(result.toString()));
 },"test-probe").start();}
}
