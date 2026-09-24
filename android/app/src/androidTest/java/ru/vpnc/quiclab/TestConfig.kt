package ru.vpnc.quiclab

import androidx.test.platform.app.InstrumentationRegistry

internal object TestConfig {
    val host: String get() = requireNotNull(InstrumentationRegistry.getArguments().getString("host")) { "Pass -e host your.domain for network tests" }
    val endpoint: String get() = InstrumentationRegistry.getArguments().getString("server") ?: "$host:4433"
}
