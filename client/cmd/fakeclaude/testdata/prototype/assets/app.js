const n = Number(localStorage.getItem("clicks") || 0);
const v = Number(localStorage.getItem("visits") || 0) + 1;
localStorage.setItem("visits", v);
document.getElementById("visits").textContent = v;
const b = document.getElementById("count");
let c = n;
b.textContent = "Clicked " + c + " times";
b.addEventListener("click", () => {
  c++;
  localStorage.setItem("clicks", c);
  b.textContent = "Clicked " + c + " times";
});
