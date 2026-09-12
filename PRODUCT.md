# Pousadinha Catalog
<!-- impeccable:product-schema 1 -->

## Platform
web

## Stack
User delegated the interface choice and confirmed integration with the existing Go server. Embedded HTML, CSS and JavaScript, with no frontend build dependency.

## Users
The bot operator curates the shared character catalog used by the Discord gacha.

## Product Purpose
Explore the current PostgreSQL catalog, add and edit characters, manage local portraits and GIFs, and keep a shared editorial favorites selection separate from AniList likes.

## Operating Context
The existing Go bot has guild-isolated virtual balances, a global character catalog, AniList metadata import, and locally rendered 420×600 assets. The private web panel requires login. Catalog edits must preserve player ownership and economy data.

## Capabilities and Constraints
Search, filters, pagination, editorial favorites, character editing, imports, photo uploads, photo review and reversible removal. Character sources must support future games. English UI follows the user's earlier preference. No production mutations for demonstration data.

## Evidence on Hand
Existing catalog and media live in PostgreSQL and data/gacha. No established web visual system exists.

## Product Principles
Make the character art easy to scan. Keep curation separate from provider popularity. Make destructive-looking actions recoverable. Use the same catalog and images as the bot.
