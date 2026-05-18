# Claude Code

1. Run `swobu`.
2. Configure provider/backend in cockpit.
3. Copy the local endpoint from Swobu.
4. Point Claude Code to that endpoint.

If request behavior differs by endpoint family (`/v1/messages` vs OpenAI-style families), verify the client mode and route are aligned.
