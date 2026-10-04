package ru.vpnc.quicprobe;

import android.app.*;
import android.content.Intent;
import android.os.*;
import java.io.*;
import java.net.*;
import java.nio.charset.StandardCharsets;
import javax.net.ssl.HttpsURLConnection;
import org.json.JSONObject;

// Separate UID makes the probe traverse the VPN; the VPN app itself is excluded.
public class SoakService extends Service {
 private volatile boolean running;
 public IBinder onBind(Intent i){return null;}
 public int onStartCommand(Intent i,int flags,int startId){
  if(running)return START_NOT_STICKY;
  if(i==null){stopSelf();return START_NOT_STICKY;}
  String id=i.getStringExtra("id"),host=i.getStringExtra("host");
  long duration=i.getLongExtra("duration_ms",60000);
  if(id==null||!id.matches("[a-zA-Z0-9_-]{1,64}")||host==null||!host.matches("[A-Za-z0-9.-]+")||duration<30000||duration>43200000){stopSelf();return START_NOT_STICKY;}
  NotificationManager nm=getSystemService(NotificationManager.class);nm.createNotificationChannel(new NotificationChannel("soak","VPN test traffic",NotificationManager.IMPORTANCE_LOW));
  startForeground(1,new Notification.Builder(this,"soak").setSmallIcon(android.R.drawable.stat_notify_sync).setContentTitle("VPN endurance test").setContentText("TCP + UDP, screen may stay off").build());
  running=true;
  new Thread(()->runProbe(id,host,duration),"soak-probe").start();return START_NOT_STICKY;
 }
 private void runProbe(String id,String host,long duration){
  long start=SystemClock.elapsedRealtime();int seq=0;
  try(FileOutputStream out=openFileOutput(id+".jsonl",MODE_PRIVATE)){
   while(running&&SystemClock.elapsedRealtime()-start<duration){
    JSONObject row=new JSONObject().put("elapsed_ms",SystemClock.elapsedRealtime()-start).put("seq",seq++);
    android.net.ConnectivityManager cm=getSystemService(android.net.ConnectivityManager.class);
    android.net.NetworkCapabilities caps=cm.getNetworkCapabilities(cm.getActiveNetwork());
    row.put("vpn",caps!=null&&caps.hasTransport(android.net.NetworkCapabilities.TRANSPORT_VPN));
    try{
     HttpsURLConnection c=(HttpsURLConnection)new URL("https://"+host+"/").openConnection();c.setConnectTimeout(10000);c.setReadTimeout(10000);c.setRequestProperty("Connection","close");
     try{if(c.getResponseCode()!=200)throw new IOException("HTTP status");try(InputStream in=c.getInputStream()){if(in.read()<0)throw new EOFException();}row.put("tcp_ok",true);}finally{c.disconnect();}
    }catch(Exception e){row.put("tcp_ok",false).put("tcp_error",e.getClass().getSimpleName());}
    try(DatagramSocket s=new DatagramSocket()){
     byte[] q={0x41,0x62,1,0,0,1,0,0,0,0,0,0,7,101,120,97,109,112,108,101,3,99,111,109,0,0,1,0,1};
     q[0]=(byte)(seq>>8);q[1]=(byte)seq;s.setSoTimeout(10000);s.connect(InetAddress.getByName("1.1.1.1"),53);s.send(new DatagramPacket(q,q.length));DatagramPacket a=new DatagramPacket(new byte[4096],4096);s.receive(a);
     byte[] b=a.getData();if(a.getLength()<12||b[0]!=q[0]||b[1]!=q[1]||(b[2]&128)==0||(b[3]&15)!=0)throw new IOException("DNS response");row.put("udp_ok",true);
    }catch(Exception e){row.put("udp_ok",false).put("udp_error",e.getClass().getSimpleName());}
    row.put("completed_ms",SystemClock.elapsedRealtime()-start);out.write((row+"\n").getBytes(StandardCharsets.UTF_8));out.flush();out.getFD().sync();
    Thread.sleep(10000);
   }
  }catch(Exception ignored){}finally{running=false;stopSelf();}
 }
 public void onDestroy(){running=false;super.onDestroy();}
}
