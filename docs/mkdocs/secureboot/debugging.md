# Signing kernel modules using KMM

If your driver containers end up in `PostStartHookError` or `CrashLoopBackOff` status, and `kubectl describe` shows an event: `modprobe: ERROR: could not insert '<your kmod name>': Required key not available` then the kmods are either not signed, or signed with the wrong key.

