package ru.vpnc.quiclab

import android.content.Context
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.*
import org.junit.Test

class PreferenceDraftTest {
    @Test fun draftIsIsolatedAndPersistsOnlyEditedKeys() {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val source = context.getSharedPreferences("test_draft", Context.MODE_PRIVATE)
        try {
            source.edit().clear().putString("endpoint", "before").putString("external", "one")
                .putStringSet("apps", setOf("a")).commit()
            val draft = PreferenceDraft(source)
            draft.edit().putString("endpoint", "after").apply()
            assertTrue(draft.dirty)
            assertEquals("before", source.getString("endpoint", null))
            draft.getStringSet("apps", null)!!.add("b")
            assertEquals(setOf("a"), draft.getStringSet("apps", null))
            source.edit().putString("external", "two").commit()
            draft.persist()
            assertEquals("after", source.getString("endpoint", null))
            assertEquals("two", source.getString("external", null))
            assertFalse(draft.dirty)
            draft.edit().clear().apply()
            assertEquals("after", source.getString("endpoint", null))
        } finally { source.edit().clear().commit() }
    }
}
