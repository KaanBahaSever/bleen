import '@fontsource-variable/inter';
import '@fontsource/nunito/800.css';
import '@fontsource/jetbrains-mono/400.css';
import './app.css';
import { mount } from 'svelte';
import App from './App.svelte';

mount(App, { target: document.getElementById('app')! });
