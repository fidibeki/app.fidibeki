# Fidibeki Flutter Example

Start the existing local mobile smoke server, then launch the host-owned example:

```sh
node examples/mobile/smoke-server.mjs
flutter run
```

The app places its own feedback trigger and calls a local host session endpoint. It contains no
tenant API key and does not open the sheet automatically.
