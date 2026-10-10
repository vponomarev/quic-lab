package ru.vpnc.quiclab

import android.app.Activity
import android.content.Intent
import android.os.Bundle

/** Stable launcher target retained for old shortcuts and pending intents. */
class MainActivity : Activity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        startActivity(Intent(this, VpnActivity::class.java))
        finish()
    }
}
