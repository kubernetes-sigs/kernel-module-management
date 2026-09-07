# Helpers for a test that holds an object with a finalizer of its own.
#
# metadata.finalizers is a list and a merge patch sends a list whole, so these read it back and
# write it with only the test's entry added or taken out, under the resourceVersion they read.

TEST_HOLD_FINALIZER=tests.kmm.sigs.x-k8s.io/hold

edit_test_finalizer () {
  local target=$1 action=$2 obj rv finalizers attempt

  for attempt in 1 2 3 4 5; do
    if ! obj=$(kubectl get "${target}" --ignore-not-found -o json); then
      return 1
    fi

    if [ -z "${obj}" ]; then
      # Only a read that came back empty says it has gone; it holds nothing, but cannot be held.
      [ "${action}" = remove ] && return 0
      return 1
    fi

    rv=$(jq -r '.metadata.resourceVersion' <<< "${obj}")
    finalizers=$(jq -c --arg f "${TEST_HOLD_FINALIZER}" --arg a "${action}" '
      (.metadata.finalizers // []) as $current
      | if $a == "add" then
          (if $current | index($f) then $current else $current + [$f] end)
        else
          [$current[] | select(. != $f)]
        end' <<< "${obj}")

    if kubectl patch "${target}" --type=merge \
        -p "{\"metadata\":{\"resourceVersion\":\"${rv}\",\"finalizers\":${finalizers}}}" > /dev/null; then
      return 0
    fi

    sleep 2
  done

  return 1
}

hold_with_test_finalizer () {
  edit_test_finalizer "$1" add
}

release_test_finalizer () {
  edit_test_finalizer "$1" remove
}
