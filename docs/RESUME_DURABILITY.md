# Resume Durability

Audit of resume correctness and crash-recovery guarantees, done 2026-06-28 to
check whether anything was worth borrowing from xav's resume design (xav guide
§1.6). This records the audit so it is not re-done; the conclusion is that no
correctness change is needed.

## How reel resume works

- Workdir lives in `cfg.TempDir` (not next to the input), named
  `.reel-<inputbase>-<sha256(canonical-path)[:12]>` where the canonical path is
  abs + symlink-resolved. Pinned by `TestWorkDirNameUsesCanonicalPathHash`.
- Each chunk encodes directly to `encode/NNNN.ivf`
  (`encoder.EncodeChunkToIVF`).
- On successful encode return + `os.Stat`, the chunk is appended to `done.txt`
  as `idx frames size` (`chunk.AppendDone`, encode.go:216, target_quality.go:320).
- On resume, `ResumeInf.Validate` re-checks **every** `done.txt` entry against
  the current chunk plan AND `os.Stat(IVFPath)` with an **exact size match**
  (`validChunkFile`). Any entry that fails existence, frame, or size check is
  dropped and re-encoded.
- A second identity gate: `resume.json` (`ResumeManifest`) and `chunk-plan`
  metadata both store `InputPath + InputSize + InputModTimeUnixNano` and reject
  resume if a fresh manifest does not `reflect.DeepEqual` the stored one.

## Audit findings (vs xav's three claims)

| xav claim | Reel status |
|-----------|-------------|
| Partial/in-flight chunks re-encoded from scratch | **Met** (via the `Validate` size check on every done entry, not via atomic writes). A partial or missing chunk never counts as done. |
| Content-hash binding (input change forces fresh workdir) | **Partially met.** Identity is path + size + mtime, not a content hash. Defeats a same-size file with a touched mtime. |
| Workdir next to the input regardless of cwd | **Handled differently, by design.** Reel uses TempDir for library/Spindle embedding; the path-hash name + symlink resolution provide equivalent binding to the input identity. Not a gap. |

**Conclusion: resume is correct.** A partial or missing chunk is always
re-encoded because `Validate` checks existence + exact size for every done
entry. The guarantee is provided post-hoc by validation rather than by atomic
writes, but the outcome is the same.

## Open hardening items (low priority, defense-in-depth)

These are not bugs -- reel self-heals the failure modes they address -- and the
library use case (rare same-size re-rips, rare same-size on-disk corruption)
makes them low-value. Listed so a future agent knows they were considered.

- **[low] Atomic chunk write + fsync.** `EncodeChunkToIVF` writes directly to
  the final `encode/NNNN.ivf` path with no temp-rename, and `done.txt` append is
  not fsync'd. On power loss a done entry could outlive the flushed chunk data.
  The `Validate` size check still catches this on resume (the file won't match
  the recorded size), so it self-heals; atomic write + fsync would make the
  mechanism match the guarantee. Note final chunks are already fsync'd (only
  ephemeral sampled probes skip it; see PERFORMANCE_TESTING.md), so this fits
  existing thinking.
- **[low] Content-hash input identity.** Replace/augment path+size+mtime with a
  cheap chunked hash (e.g. SHA over first + last chunk) so a same-size,
  touched-mtime source replacement forces a fresh workdir. Closes the narrow
  footgun that xav's full-file hash avoids.

Pursue only if resume corruption is ever observed in practice.
