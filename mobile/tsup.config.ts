import { defineConfig } from 'tsup'

export default defineConfig({
  entry: {
    index: 'src/index.ts',
    'async-storage': 'src/async-storage.ts',
  },
  dts: true,
  format: ['esm', 'cjs'],
  clean: true,
  sourcemap: true,
  target: 'es2020',
  external: ['react', 'react-native', '@react-native-async-storage/async-storage'],
})
