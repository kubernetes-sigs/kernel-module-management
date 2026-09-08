src=%s
dst=%s
if [ ! -d "$src" ]; then
  echo "modprobedDir $src is missing or not a directory" >&2
  exit 1
fi
found=0
for p in "$src"/* "$src"/.*; do
  b=${p##*/}
  if [ "$b" = "." ] || [ "$b" = ".." ]; then
    continue
  fi
  if [ ! -e "$p" ]; then
    continue
  fi
  if [ -d "$p" ]; then
    echo "modprobedDir $src contains a subdirectory" >&2
    exit 1
  fi
  if [ -f "$p" ]; then
    found=1
  fi
done
if [ "$found" -eq 0 ]; then
  echo "modprobedDir $src contains no regular files" >&2
  exit 1
fi
mkdir -p "$dst" || exit 1
for p in "$src"/* "$src"/.*; do
  b=${p##*/}
  if [ "$b" = "." ] || [ "$b" = ".." ]; then
    continue
  fi
  if [ -f "$p" ]; then
    cp "$p" "$dst/" || exit 1
  fi
done
