import '@fontsource-variable/inter';
import '@fontsource/nunito/800.css';
import '@fontsource/jetbrains-mono/400.css';
import './app.css';
import { mount } from 'svelte';
import App from './App.svelte';

// Plain-browser development: fake the Go side (see lib/mock.ts).
if (import.meta.env.DEV && !('go' in window)) await import('./lib/mock');

mount(App, { target: document.getElementById('app')! });
