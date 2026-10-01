---
status: accepted
---

# The Phone App opens on the Session list and pushes Sessions onto a stack

The Phone App started as the web Client's layout on a small screen: the open Session filled the phone and the Session list slid in from the left as a drawer. Which Session was open lived in the main screen's state, not in navigation. On an iPhone that costs the system back gesture (a swipe from the left edge opened the drawer), hides what every Session is doing behind a tap, and keeps the app on a hand-drawn header instead of the platform's navigation bar.

We decided the Phone App opens on the Session list of the connected Paired Computer, and a Session, a subagent or the New session page is pushed onto the native stack (`/session/<id>`, `/new`). The navigation bar is the stack's own: the list's title switches computers and shows the connection, its right side holds New session and Settings, and a Session's right side keeps the Git changes, Tools and subagent buttons. A swipe from the left edge goes back. The connection to the Paired Computer moves up to the root layout, so every page shares one connection and one alert tracker; the Session on screen is the one the route names.

The idea comes from Lody iOS (github.com/Innei/lody-ios), which drops the project hierarchy for an inbox that pushes conversations. It is licensed AGPL-3.0 and Droi MIT: like Waku in ADR 0006, it is a reference for the design, and none of its code is copied.

## Considered Options

- **Keep the drawer (ChatGPT's and Claude's apps).** Right for one conversation at a time; Droi on the phone is mostly watching several Sessions run on a computer, which wants their states in view.
- **Tabs (Sessions, Settings).** Settings is visited rarely; a tab bar would take height from the transcript on every page.
- **List as root with a pushed Session (chosen).**

## Consequences

- The app reopens the Session it last showed, pushed over the list, so going back lands on the list.
- Opening a subagent pushes it; going back returns to its caller. Opening a Session already on the stack goes back to it instead of pushing it twice.
- The web build for the tests renders the same stack with react-navigation's web header; the tests go back to the list with its back button instead of opening a drawer.
- Grouping the list by what needs the user, swipe actions, a long-press menu with a preview, and the New session page as a sheet build on this and are left to later changes.
