package ru.vpnc.quiclab

import android.app.Activity
import android.app.AlertDialog
import android.content.Intent
import android.net.Uri
import androidx.test.platform.app.InstrumentationRegistry
import mobile.Mobile
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import java.io.File
import java.util.concurrent.AbstractExecutorService
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean

class MdmUserImportFlowTest {
    private val instrumentation = InstrumentationRegistry.getInstrumentation()
    private fun main(block: () -> Unit) = instrumentation.runOnMainSync(block)
    private class Queue : AbstractExecutorService() {
        val tasks = ArrayDeque<Runnable>()
        override fun execute(command: Runnable) { tasks.add(command) }
        fun next() { tasks.removeFirst().run() }
        override fun shutdown() = Unit
        override fun shutdownNow(): MutableList<Runnable> = mutableListOf()
        override fun isShutdown() = false
        override fun isTerminated() = false
        override fun awaitTermination(timeout: Long, unit: TimeUnit) = false
    }
    private fun field(flow: MdmUserImportFlow, name: String): Any? =
        MdmUserImportFlow::class.java.getDeclaredField(name).apply { isAccessible = true }.get(flow)
    private fun dialog(flow: MdmUserImportFlow) = field(flow, "dialog") as AlertDialog
    private fun cleared(flow: MdmUserImportFlow) {
        for (name in listOf("bytes", "draft", "bundle", "dialog")) assertNull(name, field(flow, name))
    }
    private fun exercise(action: (MdmImportTestActivity, MdmUserImportFlow, Queue, AtomicBoolean, MutableList<Pair<JSONObject?, String?>>) -> Unit) {
        val context = instrumentation.targetContext
        // MIUI blocks background activity launches even from instrumentation.
        // Only shell/system can launch this empty debug host (DUMP permission).
        val monitor = instrumentation.addMonitor(MdmImportTestActivity::class.java.name, null, false)
        val host = try {
            android.os.ParcelFileDescriptor.AutoCloseInputStream(instrumentation.uiAutomation.executeShellCommand(
                "am start -n ru.vpnc.quiclab/.MdmImportTestActivity"
            )).use { it.readBytes() }
            instrumentation.waitForMonitorWithTimeout(monitor, 10000) as? MdmImportTestActivity
                ?: error("Debug import host was not launched")
        } finally { instrumentation.removeMonitor(monitor) }
        instrumentation.waitForIdleSync()
        val worker = Queue()
        val allowed = AtomicBoolean(true)
        val completions = mutableListOf<Pair<JSONObject?, String?>>()
        val draft = JSONObject("""{"schema":1,"profiles":[{"id":"default","name":"Existing","settings":{"transport":"quic","endpoint":"vpn.example:443"}}],"currentProfileId":"default","enabledProfileIds":["default"],"multiple":false,"globalApps":[],"dns":{"mode":"system","profileId":"default"},"budget":{"limitBytes":0},"diagnostics":{"enabled":false,"detailed":false}}""")
        val before = draft.toString()
        val runtime = MdmConfiguration.snapshotRuntime(context).toString()
        val flow = MdmUserImportFlow(host, worker, { check(allowed.get()) }, { value, error -> completions.add(value to error) })
        try {
            main {
                // Match the real editor's secure parent window.
                host.window.addFlags(android.view.WindowManager.LayoutParams.FLAG_SECURE)
                flow.start(JSONObject().put("document", draft).toString())
            }
            assertEquals(Intent.ACTION_OPEN_DOCUMENT, host.picker?.action)
            action(host, flow, worker, allowed, completions)
            assertEquals(before, draft.toString())
            assertTrue("Runtime changed", runtime == MdmConfiguration.snapshotRuntime(context).toString())
            assertTrue(completions.all { it.first == null && it.second != null })
        } finally {
            main { flow.close(); host.finish() }
            File(context.cacheDir, "mdm-import-flow-fixture").delete()
        }
    }
    private fun select(flow: MdmUserImportFlow, bytes: ByteArray) {
        File(instrumentation.targetContext.cacheDir, "mdm-import-flow-fixture").writeBytes(bytes)
        main { flow.result(Activity.RESULT_OK, Intent().setData(Uri.parse("content://ru.vpnc.quiclab.import-test/fixture"))) }
    }
    private fun drain(worker: Queue) { worker.next(); instrumentation.waitForIdleSync() }
    private fun queueCompletion(worker: Queue) {
        val thread = Thread { worker.next() }
        thread.start(); thread.join(10000)
        assertFalse(thread.stackTrace.joinToString("\n"), thread.isAlive)
    }

    @Test fun pickerCancellationCompletesOnceAndClearsDraft() = exercise { _, flow, _, _, replies ->
        main { flow.result(Activity.RESULT_CANCELED, null); flow.result(Activity.RESULT_CANCELED, null) }
        assertEquals(1, replies.size); cleared(flow)
    }
    @Test fun passwordCancellationWipesRetainedFile() = exercise { _, flow, worker, _, replies ->
        select(flow, "age-encryption.org/v1\nsynthetic".toByteArray()); drain(worker)
        val retained = field(flow, "bytes") as ByteArray
        assertTrue("Password window must prevent screenshots", dialog(flow).window!!.attributes.flags and android.view.WindowManager.LayoutParams.FLAG_SECURE != 0)
        main { dialog(flow).getButton(AlertDialog.BUTTON_NEGATIVE).performClick() }
        instrumentation.waitForIdleSync()
        assertEquals(1, replies.size); assertTrue(retained.all { it == 0.toByte() }); cleared(flow)
    }
    @Test fun mappingCancellationCompletesWithoutCandidate() = exercise { _, flow, worker, _, replies ->
        val draft = field(flow, "draft") as JSONObject
        select(flow, Mobile.exportConfiguration(draft.toString(), """{"sections":["profiles"]}""", false).toByteArray())
        drain(worker); drain(worker)
        main { dialog(flow).listView.performItemClick(null, 1, 1) }
        main { dialog(flow).getButton(AlertDialog.BUTTON_NEGATIVE).performClick() }
        instrumentation.waitForIdleSync()
        assertEquals(1, replies.size); cleared(flow)
    }
    @Test fun pauseBeforeWorkerRejectsSelectionAndUnblocks() = exercise { _, flow, worker, allowed, replies ->
        select(flow, "age-encryption.org/v1\nsynthetic".toByteArray())
        allowed.set(false); drain(worker)
        assertEquals(1, replies.size); cleared(flow)
    }
    @Test fun pauseAfterReadRejectsQueuedCompletion() = exercise { _, flow, worker, allowed, replies ->
        select(flow, "age-encryption.org/v1\nsynthetic".toByteArray())
        main {
            queueCompletion(worker)
            allowed.set(false)
        }
        instrumentation.waitForIdleSync()
        assertEquals(1, replies.size); cleared(flow)
    }
    @Test fun closeAfterReadDiscardsQueuedCompletion() = exercise { _, flow, worker, _, replies ->
        select(flow, "age-encryption.org/v1\nsynthetic".toByteArray())
        main {
            queueCompletion(worker)
            flow.close()
        }
        instrumentation.waitForIdleSync()
        assertTrue(replies.isEmpty()); cleared(flow)
    }
}
