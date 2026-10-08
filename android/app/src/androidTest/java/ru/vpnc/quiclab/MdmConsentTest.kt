package ru.vpnc.quiclab
import android.content.Intent
import android.net.Uri
import android.util.Base64
import android.view.View
import android.view.ViewGroup
import android.widget.CheckBox
import android.widget.TextView
import androidx.test.runner.lifecycle.ActivityLifecycleMonitorRegistry
import androidx.test.runner.lifecycle.Stage
import androidx.test.platform.app.InstrumentationRegistry
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test

class MdmConsentTest {
 @Test fun openingInvitationDoesNotEnrollOrGrantPermissions(){
  val inst=InstrumentationRegistry.getInstrumentation();val context=inst.targetContext
  val before=MdmStore(context).read()
  assumeTrue("Requires a dedicated unbound device",before.binding==null && !before.pendingEnrollment)
  val data=JSONObject().put("version",1).put("endpoint","https://mdm-consent.invalid")
   .put("token","a".repeat(64)).put("mode","external")
   .put("requestedRights",MdmRights(config=true,vpn=true,geo=true,coordinates=true).json())
  val link="quiclab://mdm/enroll#"+Base64.encodeToString(data.toString().toByteArray(),Base64.URL_SAFE or Base64.NO_WRAP or Base64.NO_PADDING)
  inst.uiAutomation.executeShellCommand("am start -W -a android.intent.action.VIEW -n ru.vpnc.quiclab/.MdmEnrollActivity -d "+link)
   .use{android.os.ParcelFileDescriptor.AutoCloseInputStream(it).readBytes()}
  var opened:MdmEnrollActivity?=null
  repeat(40){
   inst.runOnMainSync{opened=ActivityLifecycleMonitorRegistry.getInstance().getActivitiesInStage(Stage.RESUMED)
    .filterIsInstance<MdmEnrollActivity>().firstOrNull()}
   if(opened==null)Thread.sleep(100)
  }
  val activity=requireNotNull(opened){"MDM consent screen did not resume"}
  try{
   inst.waitForIdleSync()
   inst.runOnMainSync{
    fun all(v:View):List<View> = listOf(v)+(if(v is ViewGroup)(0 until v.childCount).flatMap{all(v.getChildAt(it))}else emptyList())
    val views=all(activity.window.decorView)
    assertTrue(views.filterIsInstance<TextView>().any{it.text.contains("mdm-consent.invalid")})
    val boxes=views.filterIsInstance<CheckBox>();assertEquals(5,boxes.size)
    assertTrue(boxes.none{it.isChecked})
   }
   assertEquals(before,MdmStore(context).read())
  }finally{inst.runOnMainSync{activity.finish()}}
 }
 @Test fun vpnLinkCannotBeUsedAsMdmInvitation(){
  for(link in listOf("vless://test@example.com:443","https://vpn.example/enroll/token","quiclab://mdm/enroll?token=secret")){
   assertTrue(runCatching{MdmInvitation.parse(link)}.isFailure)
  }
 }
}
