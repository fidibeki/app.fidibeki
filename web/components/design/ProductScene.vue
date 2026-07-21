<script setup lang="ts">
import { Check, LockKeyhole } from '@lucide/vue'

defineProps<{ replay?: boolean }>()
</script>

<template>
  <div class="product-scene" :class="{ replay }" role="img" aria-label="Storefront checkout evidence preview">
    <div class="store-header">
      <strong>NORTHSTAR</strong>
      <span>New arrivals</span>
      <span>Running</span>
      <span>Account</span>
    </div>
    <div class="store-body">
      <div class="product-photo">
        <img src="/media/storefront-runner.jpg" alt="Red running shoe shown in the Storefront checkout" />
      </div>
      <div class="checkout-panel">
        <div class="checkout-step"><Check :size="13" /> Contact</div>
        <div class="checkout-step"><Check :size="13" /> Delivery</div>
        <h3>Payment</h3>
        <p class="muted">Northstar Runner · Ember</p>
        <div class="fake-input">Card ending in 2481</div>
        <div class="fake-input">Delivery · Rua do Carmo, Lisboa</div>
        <div class="pay-button"><LockKeyhole :size="13" /> Pay EUR 128.00</div>
      </div>
    </div>
    <div v-if="replay" class="replay-marker" aria-hidden="true">
      <span />
      <b>00:18</b>
    </div>
  </div>
</template>

<style scoped>
.product-scene {
  position: relative;
  min-height: 300px;
  overflow: hidden;
  background: #f5f3ef;
  border: 1px solid var(--line);
  border-radius: var(--radius);
}

.store-header {
  display: flex;
  height: 44px;
  align-items: center;
  gap: 22px;
  padding: 0 18px;
  background: #fff;
  border-bottom: 1px solid #dedbd5;
  color: #383632;
  font-size: 10px;
}

.store-header strong {
  margin-right: auto;
  font-size: 12px;
  letter-spacing: 0.12em;
}

.store-body {
  display: grid;
  min-height: 256px;
  grid-template-columns: 1.05fr 0.95fr;
}

.product-photo {
  min-width: 0;
  overflow: hidden;
}

.product-photo img {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.checkout-panel {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 9px;
  padding: 22px;
  background: #fff;
}

.checkout-panel h3 {
  margin-top: 7px;
  font-size: 17px;
}

.checkout-step {
  display: flex;
  align-items: center;
  gap: 6px;
  color: #4d5f56;
  font-size: 10px;
  font-weight: 700;
}

.fake-input {
  padding: 9px;
  color: #5b5a55;
  border: 1px solid #dedbd5;
  border-radius: 3px;
  font-size: 9px;
}

.pay-button {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 10px;
  background: #1c332b;
  color: #fff;
  border-radius: 3px;
  font-size: 10px;
  font-weight: 700;
}

.replay-marker {
  position: absolute;
  right: 12px;
  bottom: 12px;
  left: 12px;
  display: flex;
  height: 30px;
  align-items: center;
  gap: 10px;
  padding: 0 10px;
  background: rgb(23 33 31 / 90%);
  color: #fff;
  border-radius: 4px;
  font-size: 10px;
}

.replay-marker span {
  flex: 1;
  height: 3px;
  background: linear-gradient(90deg, #54c7b9 0 38%, #69736f 38%);
  border-radius: 2px;
}

@media (max-width: 720px) {
  .product-scene {
    min-height: 250px;
  }

  .store-header span {
    display: none;
  }

  .store-body {
    min-height: 205px;
    grid-template-columns: 0.8fr 1.2fr;
  }

  .checkout-panel {
    padding: 14px;
  }
}
</style>
