package ru.vpnc.quiclab

import android.app.Activity
import android.content.ContentProvider
import android.content.ContentValues
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.os.ParcelFileDescriptor
import java.io.File

/** Empty debug-only host: never starts the VPN or loads application preferences. */
class MdmImportTestActivity : Activity() {
    var picker: Intent? = null
    override fun startActivityForResult(intent: Intent, requestCode: Int, options: Bundle?) {
        check(requestCode == MdmUserImportFlow.REQUEST)
        picker = intent
    }
}

/** Only one synthetic cache file is available to instrumentation in this process. */
class MdmImportTestProvider : ContentProvider() {
    override fun onCreate() = true
    override fun openFile(uri: Uri, mode: String): ParcelFileDescriptor {
        require(uri.path == "/fixture" && mode == "r")
        return ParcelFileDescriptor.open(File(context!!.cacheDir, "mdm-import-flow-fixture"), ParcelFileDescriptor.MODE_READ_ONLY)
    }
    override fun getType(uri: Uri) = "application/octet-stream"
    override fun query(uri: Uri, projection: Array<out String>?, selection: String?, selectionArgs: Array<out String>?, sortOrder: String?) = null
    override fun insert(uri: Uri, values: ContentValues?) = throw UnsupportedOperationException()
    override fun delete(uri: Uri, selection: String?, selectionArgs: Array<out String>?) = throw UnsupportedOperationException()
    override fun update(uri: Uri, values: ContentValues?, selection: String?, selectionArgs: Array<out String>?) = throw UnsupportedOperationException()
}
