package ru.vpnc.quiclab
import android.content.Intent
import android.view.accessibility.AccessibilityNodeInfo
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.runner.lifecycle.ActivityLifecycleMonitorRegistry
import androidx.test.runner.lifecycle.Stage
import org.junit.Assert.*
import org.junit.Test

class ConnectionLinkTest {
 @Test fun pastedEnrollmentUsesConfirmationWithoutOpeningCameraOrCreatingDevice(){
  val inst=InstrumentationRegistry.getInstrumentation();val c=inst.targetContext
  val before=VpnProfiles.list(c).map{it.id}
  inst.uiAutomation.executeShellCommand("am start -W -a android.intent.action.MAIN -c android.intent.category.LAUNCHER -n ru.vpnc.quiclab/.MainActivity").use{android.os.ParcelFileDescriptor.AutoCloseInputStream(it).readBytes()}
  Thread.sleep(500)
  inst.runOnMainSync{val a=ActivityLifecycleMonitorRegistry.getInstance().getActivitiesInStage(Stage.RESUMED).first();a.startActivity(Intent(a,ProfileScanActivity::class.java).putExtra("import_text","https://example.invalid/lab/enroll#"+"a".repeat(64)))}
  Thread.sleep(700)
  val root=inst.uiAutomation.rootInActiveWindow
  assertNotNull(root)
  assertTrue(root.findAccessibilityNodeInfosByText("Получить VPN-профиль?").isNotEmpty())
  assertTrue(root.findAccessibilityNodeInfosByText("example.invalid").isNotEmpty())
  root.findAccessibilityNodeInfosByText("Отмена").first().performAction(AccessibilityNodeInfo.ACTION_CLICK)
  Thread.sleep(300)
  assertEquals(before,VpnProfiles.list(c).map{it.id})
 }
}
