Your challenge today is to verify and validate that these tasks have been completed satisfactorily:

---
- [x] `worker.go:109-172` `processMessage()` — per-message processing with session save and callback dispatch.
  - It contains the following behaviors, each one of which needs to be tested:
    - [x] Session state after error: user message present in session
    - [x] Session saved on error path (only callback is asserted currently)
    - [x] buildSystemPrompt with AGENTS.md file present
    - [x] buildSystemPrompt with all 5 prompt files simultaneously
    - [x] buildSystemPrompt file delimiter format: `--- END FILENAME ---`
  - Code quality issues:
    - [x] Duplicated session save + error log (lines 140-148 vs 152-159) — extract `saveSession(sess)`
    - [x] Duplicated callback send structure (lines 132-138 vs 162-170) — extract `sendCallback()`
---

If we find problems here, we will start a new session to remediate what you find, in order to preserve your context.  We'll leave editing or changing files to that new session.

I have the following questions about the tasks above:

  * Are all the test functions actually functioning correctly and not silently misconfigured?
  * Is the constructor used anywhere other than in these files?
  
  Ongoing concerns:

  * Were principles in `Software Engineering at Google` and `The Pragmatic Programmer` applied here?
  * What would Sandi Metz and Martin Fowler think about the work as presented?
  