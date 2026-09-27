on open location incomingURL
    set helper to (POSIX path of (path to me)) & "Contents/Resources/quic-lab-capture"
    do shell script (quoted form of helper) & " -open " & (quoted form of incomingURL) & " >/dev/null 2>&1 &"
end open location
on run
    display dialog "Open the Wireshark page in your QUIC Lab admin, then click Open in Wireshark." buttons {"OK"} default button "OK"
end run
