package ru.vpnc.quiclab

import android.view.View
import android.view.ViewGroup

/** Stable IDs allow Android to restore editable controls after recreation. */
internal object EditorViewState {
    fun assign(root: View) {
        var next = 0x00ee0000
        fun visit(view: View) {
            if (view.id == View.NO_ID) view.id = next++
            if (view is ViewGroup) for (i in 0 until view.childCount) visit(view.getChildAt(i))
        }
        visit(root)
    }
}
