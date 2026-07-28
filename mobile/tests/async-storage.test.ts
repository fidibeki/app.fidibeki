import AsyncStorage from '@react-native-async-storage/async-storage'
import { createAsyncStorageAdapter } from '../src/async-storage'

describe('AsyncStorage adapter', () => {
  it('delegates only the explicit storage contract', async () => {
    const adapter = createAsyncStorageAdapter()
    await adapter.setItem('key', 'value')
    await expect(adapter.getItem('key')).resolves.toBe('value')
    await adapter.removeItem('key')
    await expect(adapter.getItem('key')).resolves.toBeNull()
    expect(AsyncStorage.setItem).toHaveBeenCalledWith('key', 'value')
  })
})
