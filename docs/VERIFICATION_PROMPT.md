Your challenge today is to verify and validate that these tasks have been completed satisfactorily:

- [x] `agent.go:255-360` `summarizeContext()` — summarization flow.
  - It contains the following behaviors, each one of which needs to be tested:
    - [x] Empty Content fallback to ReasoningContent (lines 314-316)
    - [x] Summary message structure: Role=Assistant, Content="", ReasoningContent="[Summary...]\n<text>"
    - [x] Summarization with attachment-protected messages through full flow
  - Code quality issues:
    - [x] Duplicated error-handling pattern (lines 294-310 vs 317-332): both do `logger.Error` + `channelLogger.Log` + session append — extract `logAndRecordSummarizationError()`
    - [x] Repeated channelLogger.Log pattern with identical Entry struct appears 4 times — deduplicate

If we find problems here, we will start a new session to remediate what you find, in order to preserve your context.  We'll leave editing or changing files to that new session.

I have the following questions about the tasks above:

  * Is the file `summary.md`'s contents sent to the LLM when the summarizer is called both in tests and for real?
  * Are the error handling patterns deduplicated correctly?
  * Is the channel logging pattern deduplicated correctly?
  
  Ongoing concerns:

  * Were principles in `Software Engineering at Google` and `The Pragmatic Programmer` applied here?
  