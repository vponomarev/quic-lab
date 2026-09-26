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
 new Thread(()->{JSONObject result=new JSONObject();try{
 result.put("id",id).put("kind",kind).put("uid",android.os.Process.myUid());
 byte[] payload=("quic-lab-probe-"+id).getBytes(StandardCharsets.UTF_8);String reply;
 if("udp".equals(kind)){try(DatagramSocket s=new DatagramSocket()){s.setSoTimeout(5000);s.connect(InetAddress.getByName(host),port);s.send(new DatagramPacket(payload,payload.length));byte[] buf=new byte[1024];DatagramPacket p=new DatagramPacket(buf,buf.length);s.receive(p);reply=new String(buf,0,p.getLength(),StandardCharsets.UTF_8);}}
 else if("https".equals(kind)){HttpsURLConnection c=(HttpsURLConnection)new URL("https://"+host+"/").openConnection();c.setConnectTimeout(5000);c.setReadTimeout(5000);c.setRequestProperty("Connection","close");try(InputStream in=c.getInputStream()){reply=new String(readN(in,100),StandardCharsets.UTF_8);}finally{c.disconnect();}}
 else{try(Socket s=new Socket()){s.connect(new InetSocketAddress(host,port),5000);s.setSoTimeout(5000);s.getOutputStream().write(payload);byte[] buf=readN(s.getInputStream(),payload.length);reply=new String(buf,StandardCharsets.UTF_8);}}
 result.put("ok", "https".equals(kind)?!reply.trim().isEmpty():reply.equals(new String(payload,StandardCharsets.UTF_8))).put("reply",reply);
 }catch(Exception e){try{result.put("ok",false).put("error",e.getClass().getSimpleName()+": "+e.getMessage());}catch(Exception ignored){}}
 try(FileOutputStream out=openFileOutput(id+".json",MODE_PRIVATE)){out.write(result.toString().getBytes(StandardCharsets.UTF_8));}catch(Exception ignored){}
 runOnUiThread(()->text.setText(result.toString()));
 },"test-probe").start();}
}
