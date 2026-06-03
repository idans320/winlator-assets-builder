#version 450

layout(location = 0) in vec2 fragUV;
layout(location = 0) out vec4 outColor;

void main() {
    float v = fragUV.x * fragUV.y;
    for (int i = 0; i < 200; i++) {
        v = sin(v) * cos(v) + sqrt(abs(v) + 0.001);
        v = fract(v * 1337.0);
    }
    outColor = vec4(v, fract(v * 255.0), fract(v * 65535.0), 1.0);
}
