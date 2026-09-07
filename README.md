# I’ll Be Back 🌴

Etiquette in corporate jobs and startups is to indicate when you're away. Especially if you're customer facing. But for some reason I always forget, so I made this [app](https://ill-be-back-production.up.railway.app) to set the out-of-office status on all my apps in one click. While I'm at it, I also recommend [Slapss](https://slapss-app.com/), which helps not being late in meetings, which is also frowned upon.

Vibe coded with Codex + Astra. Go on the back, React on the front.

## Run it locally

You’ll need Go 1.26+ and Node 22+.

```sh
cd frontend
npm ci
npm run build
cd ..
go run ./cmd/server -dev
```

Open [localhost:8000](http://localhost:8000). To connect real accounts, set the OAuth credentials listed in [.env.example](.env.example) in your shell; `.env` files aren’t loaded automatically.

For the fiddly bits: [Google setup](docs/google-verification.md), [Microsoft setup](docs/microsoft-setup.md), and the [Slack app manifest](deploy/slack-manifest.json).
