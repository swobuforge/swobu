# LM Studio backend

Start the LM Studio local server, then select LM Studio in Cockpit. Swobu uses
`http://127.0.0.1:1234/v1` by default. Custom base URLs must end in `/v1`.

Choose a discovered generative model and protocol. Authentication is optional;
when configured, Swobu sends the credential as an `Authorization: Bearer`
token.
