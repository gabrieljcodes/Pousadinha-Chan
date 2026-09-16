# Claim consistency and Discord message delivery

## Incident review (2026-09-15)

Read-only inspection of the affected Robin Nico roll confirmed that its claim
receipt and the server collection had the same winner. The Discord message still
had its original claim button and no winner. Its last edit was at 18:53:49 UTC,
before the later repeated clicks at 18:53:58 UTC and beyond. This does not establish
that a losing click overwrote the winning edit: the old handler discarded public
edit errors, so the precise delivery failure cannot be reconstructed.

The wishlist banner matched a stored wish from a third server member. Wishes are
server-wide notifications; neither the roller nor the people attempting a claim
need to have wished for the character. The label now explicitly says
“Wishlisted in this server”. A wish does not reserve ownership or override claim
cooldowns. Wishlist records have no creation/removal audit history, so a current
row cannot establish exactly when it was added.

## Guarantees

- PostgreSQL remains the ownership authority. Player row locks, the conditional
  roll update and the unique server/character collection key admit one winner.
  Losing claims roll back and do not consume claim availability.
- Discord acknowledgement happens before claim processing. A public message edit
  failure does not roll back a committed claim or make the reward available again.
- Public updates use the committed roll's immutable `claimed_by` receipt, scoped
  to the server and channel. A delayed or losing click can repair an earlier failed
  edit, but cannot publish itself as the winner or re-enable the claim button.
- Reconciliation uses its own bounded context. A database commit timeout is not
  treated as proof that the transaction failed.
- The public message contains the recorded winner, a disabled button and a
  dedicated claim field. Rendering does not mutate the interaction snapshot and
  repeated rendering does not duplicate the field or ping wishlist members again.
- Claim acknowledgement, private response and public update failures are logged
  with roll/message identifiers for diagnosis.
- Gem claims acknowledge before database work, verify the channel as well as the
  server, check expiry after acquiring locks, and use player-before-roll lock order.
  A gem roll cannot be claimed through the character-claim endpoint.
- Roll messages do not include empty component rows. Wishlist query errors are
  reported rather than returning an incomplete list of mentions.

## Validation

Integration tests against disposable PostgreSQL combine concurrent claim clicks,
a simulated failed Discord public edit, and a later stale click. They assert a
single owner, a single consumed cooldown, consistent winner rendering, disabled
buttons, preserved input snapshots and guild/channel isolation. Gem tests cover
concurrent awards, channel isolation and rejection of character claims on gem rolls.

## Remaining improvements

1. Add a durable message-update outbox with channel/message IDs, bounded retries
   and latest-state rendering. Currently a failed public edit is repaired by a
   subsequent click; there is no persistent background retry after a restart.
2. Distinguish cooldown, expired roll, already claimed and hidden-character failures
   in player-facing responses instead of the combined claim error.
3. Add an explicit wish-removal policy after a successful claim and per-user wish
   notification preferences. Current wishes persist until removed by the user.
4. Track acknowledgement latency, claim transaction latency, edit failures and
   reconciliation results, so delivery errors are visible before players report them.

Discord delivery and PostgreSQL commits are not an atomic distributed operation.
The ownership record must remain correct even when Discord is unavailable. A
claim receipt describes the original claim; later trades or divorces are separate
collection operations and do not rewrite that historical receipt.
