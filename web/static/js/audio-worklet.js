/**
 * AudioWorklet processor that resamples audio to 16kHz mono PCM
 * for Vosk speech recognition.
 *
 * Uses fractional resampling to handle any input sample rate correctly.
 */
class PCMProcessor extends AudioWorkletProcessor {
  constructor() {
    super();
    this.inputSampleRate = sampleRate; // AudioWorklet global (e.g., 48000 or 44100)
    this.outputSampleRate = 16000;

    // Fractional step: how many input samples per output sample
    this.step = this.inputSampleRate / this.outputSampleRate;
    this.inputIndex = 0; // Fractional position in input stream

    // Input buffer to accumulate samples
    this.inputBuffer = [];

    // Output buffer - send chunks of ~100ms (1600 samples at 16kHz = 3200 bytes)
    this.outputChunkSize = 1600;
    this.outputBuffer = new Int16Array(this.outputChunkSize);
    this.outputIndex = 0;

    // Log configuration
    this.port.postMessage({
      type: 'config',
      inputRate: this.inputSampleRate,
      outputRate: this.outputSampleRate,
      step: this.step
    });
  }

  process(inputs, outputs, parameters) {
    const input = inputs[0];
    if (!input || !input[0]) {
      return true;
    }

    // Append new samples to input buffer
    const inputData = input[0];
    for (let i = 0; i < inputData.length; i++) {
      this.inputBuffer.push(inputData[i]);
    }

    // Resample using linear interpolation
    while (this.inputIndex < this.inputBuffer.length - 1) {
      const idx = Math.floor(this.inputIndex);
      const frac = this.inputIndex - idx;

      // Linear interpolation between two samples
      const sample0 = this.inputBuffer[idx];
      const sample1 = this.inputBuffer[idx + 1];
      const sample = sample0 + frac * (sample1 - sample0);

      // Convert Float32 [-1, 1] to Int16 [-32768, 32767]
      const clamped = Math.max(-1, Math.min(1, sample));
      const pcm = Math.round(clamped * 32767);
      this.outputBuffer[this.outputIndex++] = pcm;

      // Advance by fractional step
      this.inputIndex += this.step;

      // Send when output buffer is full
      if (this.outputIndex >= this.outputChunkSize) {
        const dataToSend = new Int16Array(this.outputBuffer);
        this.port.postMessage({
          type: 'pcm',
          data: dataToSend.buffer
        }, [dataToSend.buffer]);
        this.outputIndex = 0;
      }
    }

    // Remove consumed samples from input buffer
    const consumed = Math.floor(this.inputIndex);
    if (consumed > 0) {
      this.inputBuffer.splice(0, consumed);
      this.inputIndex -= consumed;
    }

    return true;
  }
}

registerProcessor('pcm-processor', PCMProcessor);
